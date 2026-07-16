/**
 * Resolves a Codex rollout jsonl path from a session ID or cwd.
 *
 * Filenames follow rollout-<timestamp>-<id>.jsonl. When ID-based lookup fails
 * (Herdr session drift, thread vs root id), falls back to session_meta ids and
 * then the most recent rollout whose session_meta.cwd matches the pane cwd
 * (with path normalization; basename as last resort).
 */
package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/fsutil"
	"github.com/senna-lang/herdr-agent-usage/internal/pathutil"
)

func codexHome() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func sessionsRoot() string {
	return filepath.Join(codexHome(), "sessions")
}

type rolloutCandidate struct {
	path    string
	mtimeMs int64
}

func listRolloutCandidates() []rolloutCandidate {
	root := sessionsRoot()
	var matches []rolloutCandidate
	years, err := os.ReadDir(root)
	if err != nil {
		return matches
	}
	for _, year := range years {
		if !year.IsDir() {
			continue
		}
		yearPath := filepath.Join(root, year.Name())
		months, err := os.ReadDir(yearPath)
		if err != nil {
			continue
		}
		for _, month := range months {
			if !month.IsDir() {
				continue
			}
			monthPath := filepath.Join(yearPath, month.Name())
			days, err := os.ReadDir(monthPath)
			if err != nil {
				continue
			}
			for _, day := range days {
				if !day.IsDir() {
					continue
				}
				dayPath := filepath.Join(monthPath, day.Name())
				files, err := os.ReadDir(dayPath)
				if err != nil {
					continue
				}
				for _, name := range files {
					n := name.Name()
					if !strings.HasPrefix(n, "rollout-") || !strings.HasSuffix(n, ".jsonl") {
						continue
					}
					full := filepath.Join(dayPath, n)
					st, err := os.Stat(full)
					if err != nil || !st.Mode().IsRegular() {
						continue
					}
					matches = append(matches, rolloutCandidate{path: full, mtimeMs: st.ModTime().UnixMilli()})
				}
			}
		}
	}
	return matches
}

// FindSessionFile finds a rollout whose filename ends with sessionId.jsonl.
// When multiple match, prefers the most recently modified file.
func FindSessionFile(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	suffix := sessionID + ".jsonl"
	var matches []rolloutCandidate
	for _, c := range listRolloutCandidates() {
		if strings.HasSuffix(c.path, suffix) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].mtimeMs > matches[j].mtimeMs })
	return matches[0].path
}

type sessionMeta struct {
	cwd       string
	sessionID string
	id        string
}

func readSessionMeta(path string) sessionMeta {
	first, err := fsutil.ReadFirstLine(path, 1024*1024)
	if err != nil {
		return sessionMeta{}
	}
	var parsed struct {
		Type    string `json:"type"`
		Payload *struct {
			Cwd       string `json:"cwd"`
			SessionID string `json:"session_id"`
			ID        string `json:"id"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(first), &parsed); err != nil {
		return sessionMeta{}
	}
	if parsed.Type != "session_meta" || parsed.Payload == nil {
		return sessionMeta{}
	}
	return sessionMeta{
		cwd:       parsed.Payload.Cwd,
		sessionID: parsed.Payload.SessionID,
		id:        parsed.Payload.ID,
	}
}

func readSessionMetaCwd(path string) string {
	return readSessionMeta(path).cwd
}

// FindSessionFileByMetaID finds a rollout whose session_meta session_id or id
// equals sessionID (thread/root id drift when the filename suffix differs).
func FindSessionFileByMetaID(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	var matches []rolloutCandidate
	for _, c := range listRolloutCandidates() {
		meta := readSessionMeta(c.path)
		if meta.sessionID == sessionID || meta.id == sessionID {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].mtimeMs > matches[j].mtimeMs })
	return matches[0].path
}

func pickNewest(matches []rolloutCandidate) string {
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].mtimeMs > matches[j].mtimeMs })
	return matches[0].path
}

// FindLatestSessionFileForCwd returns the rollout with the latest mtime among
// those whose session_meta.cwd matches the pane cwd (normalized; basename fallback).
func FindLatestSessionFileForCwd(cwd string) string {
	if cwd == "" {
		return ""
	}
	var exact []rolloutCandidate
	var weak []rolloutCandidate
	for _, candidate := range listRolloutCandidates() {
		metaCwd := readSessionMetaCwd(candidate.path)
		if metaCwd == "" {
			continue
		}
		if pathutil.Equal(metaCwd, cwd) {
			exact = append(exact, candidate)
			continue
		}
		if pathutil.SameProject(metaCwd, cwd) {
			weak = append(weak, candidate)
		}
	}
	if path := pickNewest(exact); path != "" {
		return path
	}
	return pickNewest(weak)
}

// ResolveSessionFile prefers sessionId lookup, then meta id, then latest cwd rollout.
func ResolveSessionFile(sessionID, cwd *string) string {
	if sessionID != nil && *sessionID != "" {
		if byID := FindSessionFile(*sessionID); byID != "" {
			return byID
		}
		if byMeta := FindSessionFileByMetaID(*sessionID); byMeta != "" {
			return byMeta
		}
	}
	if cwd != nil && *cwd != "" {
		return FindLatestSessionFileForCwd(*cwd)
	}
	return ""
}
