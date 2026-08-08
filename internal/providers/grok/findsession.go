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

// FindActiveSessionID returns a session_id for cwd only when the match is
// unambiguous. Multiple concurrent Grok panes often share the same cwd
// (especially $HOME); picking the "most recently opened" would pin every
// unbound pane to one sibling's context meter.
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
	if len(pick) != 1 {
		// 0 → nothing; 2+ → ambiguous (do not guess).
		return ""
	}
	return pick[0].SessionID
}

// isActiveSessionID reports whether sessionID appears in active_sessions.json.
// Herdr only binds agent_session at session_start; after resume/switch the
// bound id goes stale while active_sessions stays current.
func isActiveSessionID(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	for _, entry := range readActiveSessions() {
		if entry.SessionID == sessionID {
			return true
		}
	}
	return false
}

// sessionDirForID resolves a session directory, preferring the pane cwd group.
func sessionDirForID(sessionID string, cwd *string) string {
	if sessionID == "" {
		return ""
	}
	if cwd != nil && *cwd != "" {
		direct := filepath.Join(sessionsRoot(), encodeCwd(*cwd), sessionID)
		if st, err := os.Stat(direct); err == nil && st.IsDir() {
			return direct
		}
	}
	return FindSessionDirBySessionID(sessionID)
}

// sessionsInGroup returns every session id under groupDir that has usable
// context files (signals.json and/or updates.jsonl), plus the newest id/mtime
// for callers that still need a single-session pick.
func sessionsInGroup(groupDir string) (ids []string, newestID string, newestMT int64) {
	names, err := os.ReadDir(groupDir)
	if err != nil {
		return nil, "", 0
	}
	for _, name := range names {
		if !name.IsDir() {
			continue
		}
		dir := filepath.Join(groupDir, name.Name())
		if !sessionDirHasUsageFiles(dir) {
			continue
		}
		id := name.Name()
		ids = append(ids, id)
		// Prefer the newest usage file mtime within the session.
		mt := int64(0)
		for _, fname := range []string{"signals.json", "updates.jsonl"} {
			if st, err := os.Stat(filepath.Join(dir, fname)); err == nil {
				if t := st.ModTime().UnixMilli(); t > mt {
					mt = t
				}
			}
		}
		if newestID == "" || mt > newestMT {
			newestID = id
			newestMT = mt
		}
	}
	return ids, newestID, newestMT
}

func newestSessionInGroup(groupDir string) (sessionID string, mtimeMs int64) {
	_, sessionID, mtimeMs = sessionsInGroup(groupDir)
	return sessionID, mtimeMs
}

// uniqueSessionInGroup returns the sole session under groupDir, or "" when
// zero/multiple sessions exist. Multi-session groups are common under $HOME;
// "newest by mtime" there steals another pane's meter (e.g. a busy coding
// agent at 146k while a fresh brew-scan pane is only 23k).
func uniqueSessionInGroup(groupDir string) string {
	ids, _, _ := sessionsInGroup(groupDir)
	if len(ids) != 1 {
		return ""
	}
	return ids[0]
}

// FindLatestSessionIDUnderCwd returns a session only when the cwd group has
// exactly one session with signals.json. Previously it returned the newest
// by mtime, which mis-attributed context across panes sharing a cwd.
func FindLatestSessionIDUnderCwd(cwd *string) string {
	if cwd == nil || *cwd == "" {
		return ""
	}
	// Fast path: encoded pane cwd directory.
	if id := uniqueSessionInGroup(filepath.Join(sessionsRoot(), encodeCwd(*cwd))); id != "" {
		return id
	}
	// Scan all groups: decode folder names and match with pathutil.
	// Only accept a unique session within an exact (then weak) match group.
	root := sessionsRoot()
	groups, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var exactIDs, weakIDs []string
	for _, group := range groups {
		if !group.IsDir() {
			continue
		}
		decoded, err := url.QueryUnescape(group.Name())
		if err != nil || decoded == "" {
			continue
		}
		id := uniqueSessionInGroup(filepath.Join(root, group.Name()))
		if id == "" {
			// Group has 0 or 2+ sessions — never invent a pick from mtime.
			continue
		}
		if pathutil.Equal(decoded, *cwd) {
			exactIDs = append(exactIDs, id)
			continue
		}
		if pathutil.SameProject(decoded, *cwd) {
			weakIDs = append(weakIDs, id)
		}
	}
	if len(exactIDs) == 1 {
		return exactIDs[0]
	}
	if len(exactIDs) == 0 && len(weakIDs) == 1 {
		return weakIDs[0]
	}
	return ""
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

// FindSessionDirBySessionID searches every cwd group for a session directory.
// The dir may lack signals.json (some live sessions only stream updates.jsonl).
func FindSessionDirBySessionID(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	root := sessionsRoot()
	groups, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, group := range groups {
		if !group.IsDir() {
			continue
		}
		candidate := filepath.Join(root, group.Name(), sessionID)
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}
	return ""
}

// FindSignalsPathBySessionID searches every cwd group for a session_id match.
func FindSignalsPathBySessionID(sessionID string) string {
	dir := FindSessionDirBySessionID(sessionID)
	if dir == "" {
		return ""
	}
	candidate := filepath.Join(dir, "signals.json")
	if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() {
		return candidate
	}
	return ""
}

// sessionDirHasUsageFiles reports whether the dir can yield a context meter
// (signals and/or updates). Empty dirs must not win unique-session fallbacks.
func sessionDirHasUsageFiles(dir string) bool {
	for _, name := range []string{"signals.json", "updates.jsonl"} {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			return true
		}
	}
	return false
}

// ResolveSessionDir resolves the on-disk session directory for a pane.
//
// Priority:
//  1. Explicit session id (Herdr agent_session):
//     - If still listed in active_sessions.json → use bound (multi-pane same
//     cwd must not steal each other's meter).
//     - If stale (absent from active_sessions) and FindActiveSessionID(cwd)
//     yields a unique active id different from bound → recover to that dir.
//     - Otherwise stick with bound (prefer stale/empty over guessing sibling).
//  2. Without a bound id: only when the cwd group has exactly one session with
//     usage files. We deliberately do NOT trust active_sessions alone here —
//     Grok often records a single $HOME entry while many panes share that cwd,
//     and using it pins every unbound pane to the wrong meter.
func ResolveSessionDir(sessionID, cwd *string) string {
	if sessionID != nil && *sessionID != "" {
		// Stale bind recovery: Grok herdr hook only reports session id at
		// session_start; after resume/switch, active_sessions is authoritative
		// when the cwd match is unique.
		if !isActiveSessionID(*sessionID) {
			if active := FindActiveSessionID(cwd); active != "" && active != *sessionID {
				if dir := sessionDirForID(active, cwd); dir != "" {
					return dir
				}
			}
		}
		return sessionDirForID(*sessionID, cwd)
	}

	if cwd == nil || *cwd == "" {
		return ""
	}
	uniqueID := FindLatestSessionIDUnderCwd(cwd)
	if uniqueID == "" {
		return ""
	}
	if group := resolveGroupDirForCwd(*cwd); group != "" {
		dir := filepath.Join(group, uniqueID)
		if st, err := os.Stat(dir); err == nil && st.IsDir() && sessionDirHasUsageFiles(dir) {
			return dir
		}
	}
	dir := filepath.Join(sessionsRoot(), encodeCwd(*cwd), uniqueID)
	if st, err := os.Stat(dir); err == nil && st.IsDir() && sessionDirHasUsageFiles(dir) {
		return dir
	}
	return FindSessionDirBySessionID(uniqueID)
}

// ResolveSignalsPath resolves the signals.json path from session id and/or cwd.
// Kept for tests/callers that only care about signals; live resolution uses
// ResolveSessionDir + UsageFromSessionDir so updates.jsonl can fill gaps.
func ResolveSignalsPath(sessionID, cwd *string) string {
	dir := ResolveSessionDir(sessionID, cwd)
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "signals.json")
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
		return path
	}
	return ""
}
