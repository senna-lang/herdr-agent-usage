/**
 * Resolves the signals.json path from a Grok session ID or cwd.
 *
 * Layout:
 *   $GROK_HOME/sessions/<url-encoded-cwd>/<session-id>/signals.json
 *
 * Herdr often omits agent_session for Grok; resolution falls back to cwd.
 * Uniqueness is decided on live entries in active_sessions.json, not on
 * historical session directories. Cwd strings are compared with
 * normalization (symlink /private) and a basename fallback when the
 * project folder was renamed but the leaf name is unchanged.
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

// encodeCwd matches encodeURIComponent (spaces as %20, not +).
func encodeCwd(cwd string) string {
	return strings.ReplaceAll(url.QueryEscape(cwd), "+", "%20")
}

type activeSessionEntry struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	OpenedAt  string `json:"opened_at"`
}

func readActiveSessionsIn(home string) []activeSessionEntry {
	if home == "" {
		return nil
	}
	path := filepath.Join(home, "active_sessions.json")
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

// liveSessionIDsIn returns active session IDs for cwd. Exact path matches
// win over SameProject matches. Order is file order, not recency: uniqueness
// is the contract, not newest-wins.
func liveSessionIDsIn(home string, cwd *string) []string {
	if cwd == nil || *cwd == "" {
		return nil
	}
	var exact []string
	var weak []string
	seenExact := map[string]bool{}
	seenWeak := map[string]bool{}
	for _, entry := range readActiveSessionsIn(home) {
		if entry.SessionID == "" || entry.Cwd == "" {
			continue
		}
		switch {
		case pathutil.Equal(entry.Cwd, *cwd):
			if !seenExact[entry.SessionID] {
				exact = append(exact, entry.SessionID)
				seenExact[entry.SessionID] = true
			}
		case pathutil.SameProject(entry.Cwd, *cwd):
			if !seenWeak[entry.SessionID] {
				weak = append(weak, entry.SessionID)
				seenWeak[entry.SessionID] = true
			}
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return weak
}

func signalsPathForIDIn(home, sessionID string, cwd *string) string {
	if home == "" || sessionID == "" {
		return ""
	}
	root := filepath.Join(home, "sessions")
	if cwd != nil && *cwd != "" {
		direct := filepath.Join(root, encodeCwd(*cwd), sessionID, "signals.json")
		if st, err := os.Stat(direct); err == nil && st.Mode().IsRegular() {
			return direct
		}
	}
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

// FindActiveSessionID returns the session_id when cwd has exactly one live
// session. Multiple live sessions return empty so callers cannot attribute
// another pane's session.
func FindActiveSessionID(cwd *string) string {
	ids := liveSessionIDsIn(grokHome(), cwd)
	if len(ids) != 1 {
		return ""
	}
	return ids[0]
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
	return findLatestSessionIDUnderCwdIn(grokHome(), cwd)
}

func findLatestSessionIDUnderCwdIn(home string, cwd *string) string {
	if home == "" || cwd == nil || *cwd == "" {
		return ""
	}
	root := filepath.Join(home, "sessions")
	// Fast path: encoded pane cwd directory.
	if id, _ := newestSessionInGroup(filepath.Join(root, encodeCwd(*cwd))); id != "" {
		return id
	}
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

func resolveGroupDirForCwdIn(home, cwd string) string {
	root := filepath.Join(home, "sessions")
	direct := filepath.Join(root, encodeCwd(cwd))
	if st, err := os.Stat(direct); err == nil && st.IsDir() {
		return direct
	}
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

func historicalSignalsPathIn(home string, cwd *string) string {
	if cwd == nil || *cwd == "" {
		return ""
	}
	latestID := findLatestSessionIDUnderCwdIn(home, cwd)
	if latestID == "" {
		return ""
	}
	if group := resolveGroupDirForCwdIn(home, *cwd); group != "" {
		path := filepath.Join(group, latestID, "signals.json")
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			return path
		}
	}
	path := filepath.Join(home, "sessions", encodeCwd(*cwd), latestID, "signals.json")
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
		return path
	}
	return signalsPathForIDIn(home, latestID, cwd)
}

func containsSessionID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

// FindSignalsPathBySessionID searches every cwd group for a session_id match.
func FindSignalsPathBySessionID(sessionID string) string {
	return signalsPathForIDIn(grokHome(), sessionID, nil)
}

// ResolveSignalsPath resolves signals.json from a pane bind and/or cwd.
//
// Live sessions come from active_sessions.json, not historical directory
// counts. A bound id that is still live is kept even when other panes are
// live in the same cwd. A stale bind recovers only to a unique live
// replacement. Multiple live sessions leave an unbound pane unresolved.
// Zero live sessions fall back to the newest historical directory so a cwd
// that has been used more than once is not blanked.
func ResolveSignalsPath(sessionID, cwd *string) string {
	return resolveSignalsPathFrom(grokHome(), sessionID, cwd)
}

// ResolveSignalsPathIn resolves a pane only inside one configured GROK_HOME.
// It never falls back to another profile's session store.
func ResolveSignalsPathIn(home string, sessionID, cwd *string) string {
	return resolveSignalsPathFrom(home, sessionID, cwd)
}

func resolveSignalsPathFrom(home string, sessionID, cwd *string) string {
	live := liveSessionIDsIn(home, cwd)
	bound := ""
	if sessionID != nil {
		bound = *sessionID
	}
	boundPath := signalsPathForIDIn(home, bound, cwd)
	if bound != "" && containsSessionID(live, bound) && boundPath != "" {
		return boundPath
	}
	if len(live) == 1 {
		if path := signalsPathForIDIn(home, live[0], cwd); path != "" {
			return path
		}
	}
	if boundPath != "" {
		return boundPath
	}
	if len(live) > 1 {
		return ""
	}
	return historicalSignalsPathIn(home, cwd)
}
