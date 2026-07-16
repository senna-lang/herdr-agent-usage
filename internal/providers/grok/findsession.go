/**
 * Resolves the signals.json path from a Grok session ID or cwd.
 *
 * Layout:
 *   $GROK_HOME/sessions/<url-encoded-cwd>/<session-id>/signals.json
 *
 * Herdr often omits agent_session for Grok; resolution falls back to cwd.
 * Cwd strings are compared with normalization (symlink /private) and a
 * basename fallback when the project folder was renamed but the leaf name
 * is unchanged.
 */
package grok

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/pathutil"
)

func grokHome() string {
	if v := os.Getenv("GROK_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grok")
}

func sessionsRoot() string {
	return filepath.Join(grokHome(), "sessions")
}

// encodeCwd matches encodeURIComponent (spaces as %20, not +).
func encodeCwd(cwd string) string {
	return strings.ReplaceAll(url.QueryEscape(cwd), "+", "%20")
}

type activeSessionEntry struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	OpenedAt  string `json:"opened_at"`
}

func readActiveSessions() []activeSessionEntry {
	path := filepath.Join(grokHome(), "active_sessions.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var parsed []activeSessionEntry
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	return parsed
}

// FindActiveSessionID returns the most recent session_id whose cwd matches.
func FindActiveSessionID(cwd *string) string {
	if cwd == nil || *cwd == "" {
		return ""
	}
	var exact []activeSessionEntry
	var weak []activeSessionEntry
	for _, entry := range readActiveSessions() {
		if entry.SessionID == "" || entry.Cwd == "" {
			continue
		}
		if pathutil.Equal(entry.Cwd, *cwd) {
			exact = append(exact, entry)
		} else if pathutil.SameProject(entry.Cwd, *cwd) {
			weak = append(weak, entry)
		}
	}
	pick := exact
	if len(pick) == 0 {
		pick = weak
	}
	if len(pick) == 0 {
		return ""
	}
	sort.Slice(pick, func(i, j int) bool {
		return pick[i].OpenedAt > pick[j].OpenedAt
	})
	return pick[0].SessionID
}

func newestSessionInGroup(groupDir string) (sessionID string, mtimeMs int64) {
	names, err := os.ReadDir(groupDir)
	if err != nil {
		return "", 0
	}
	for _, name := range names {
		if !name.IsDir() {
			continue
		}
		signalsPath := filepath.Join(groupDir, name.Name(), "signals.json")
		st, err := os.Stat(signalsPath)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		mt := st.ModTime().UnixMilli()
		if sessionID == "" || mt > mtimeMs {
			sessionID = name.Name()
			mtimeMs = mt
		}
	}
	return sessionID, mtimeMs
}

// FindLatestSessionIDUnderCwd returns the session_id under the matching cwd
// group with the newest signals.json mtime.
func FindLatestSessionIDUnderCwd(cwd *string) string {
	if cwd == nil || *cwd == "" {
		return ""
	}
	// Fast path: encoded pane cwd directory.
	if id, _ := newestSessionInGroup(filepath.Join(sessionsRoot(), encodeCwd(*cwd))); id != "" {
		return id
	}
	// Scan all groups: decode folder names and match with pathutil.
	root := sessionsRoot()
	groups, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var bestExactID, bestWeakID string
	var bestExactMT, bestWeakMT int64
	for _, group := range groups {
		if !group.IsDir() {
			continue
		}
		decoded, err := url.QueryUnescape(group.Name())
		if err != nil || decoded == "" {
			continue
		}
		id, mt := newestSessionInGroup(filepath.Join(root, group.Name()))
		if id == "" {
			continue
		}
		if pathutil.Equal(decoded, *cwd) {
			if bestExactID == "" || mt > bestExactMT {
				bestExactID, bestExactMT = id, mt
			}
			continue
		}
		if pathutil.SameProject(decoded, *cwd) {
			if bestWeakID == "" || mt > bestWeakMT {
				bestWeakID, bestWeakMT = id, mt
			}
		}
	}
	if bestExactID != "" {
		return bestExactID
	}
	return bestWeakID
}

// resolveGroupDirForCwd returns the sessions/<encoded-cwd> directory for pane cwd.
func resolveGroupDirForCwd(cwd string) string {
	direct := filepath.Join(sessionsRoot(), encodeCwd(cwd))
	if st, err := os.Stat(direct); err == nil && st.IsDir() {
		return direct
	}
	root := sessionsRoot()
	groups, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var exactDir, weakDir string
	var exactMT, weakMT int64
	for _, group := range groups {
		if !group.IsDir() {
			continue
		}
		decoded, err := url.QueryUnescape(group.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(root, group.Name())
		_, mt := newestSessionInGroup(dir)
		if pathutil.Equal(decoded, cwd) {
			if exactDir == "" || mt > exactMT {
				exactDir, exactMT = dir, mt
			}
			continue
		}
		if pathutil.SameProject(decoded, cwd) {
			if weakDir == "" || mt > weakMT {
				weakDir, weakMT = dir, mt
			}
		}
	}
	if exactDir != "" {
		return exactDir
	}
	return weakDir
}

// FindSignalsPathBySessionID searches every cwd group for a session_id match.
func FindSignalsPathBySessionID(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	root := sessionsRoot()
	groups, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, group := range groups {
		candidate := filepath.Join(root, group.Name(), sessionID, "signals.json")
		if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

// ResolveSignalsPath resolves the signals.json path from session id and/or cwd.
func ResolveSignalsPath(sessionID, cwd *string) string {
	if sessionID != nil && *sessionID != "" {
		if cwd != nil && *cwd != "" {
			direct := filepath.Join(sessionsRoot(), encodeCwd(*cwd), *sessionID, "signals.json")
			if st, err := os.Stat(direct); err == nil && st.Mode().IsRegular() {
				return direct
			}
			// Cwd may have been renamed; still try the session id under any group.
		}
		if path := FindSignalsPathBySessionID(*sessionID); path != "" {
			return path
		}
	}

	activeID := FindActiveSessionID(cwd)
	if activeID != "" {
		if path := FindSignalsPathBySessionID(activeID); path != "" {
			return path
		}
	}

	if cwd == nil || *cwd == "" {
		return ""
	}
	latestID := FindLatestSessionIDUnderCwd(cwd)
	if latestID == "" {
		return ""
	}
	// Prefer group dir resolved via path matching (handles rename / encode drift).
	if group := resolveGroupDirForCwd(*cwd); group != "" {
		path := filepath.Join(group, latestID, "signals.json")
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			return path
		}
	}
	path := filepath.Join(sessionsRoot(), encodeCwd(*cwd), latestID, "signals.json")
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
		return path
	}
	// Last resort: id alone anywhere under sessions.
	return FindSignalsPathBySessionID(latestID)
}
