/**
 * Tests for Grok session resolution (uses a temporary GROK_HOME).
 */
package grok

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testCwd = "/Users/example/project"

func withTempGrokHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)
	return dir
}

func writeSession(t *testing.T, home, sessionID string, signals map[string]any, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", encodeCwd(testCwd), sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "signals.json")
	b, _ := json.Marshal(signals)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeActiveSessions(t *testing.T, home string, entries []map[string]any) {
	t.Helper()
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func signals(n int) map[string]any {
	return map[string]any{"contextTokensUsed": n, "contextWindowTokens": 500_000}
}

func TestFindActiveSessionID(t *testing.T) {
	home := withTempGrokHome(t)
	writeActiveSessions(t, home, []map[string]any{
		{"session_id": "old-id", "cwd": testCwd, "opened_at": "2026-07-14T10:00:00Z"},
		{"session_id": "new-id", "cwd": testCwd, "opened_at": "2026-07-15T10:00:00Z"},
		{"session_id": "other", "cwd": "/other", "opened_at": "2026-07-15T12:00:00Z"},
	})
	cwd := testCwd
	if got := FindActiveSessionID(&cwd); got != "" {
		t.Fatalf("got %q, want empty for multiple live sessions", got)
	}

	writeActiveSessions(t, home, []map[string]any{
		{"session_id": "only", "cwd": testCwd, "opened_at": "2026-07-15T10:00:00Z"},
		{"session_id": "other", "cwd": "/other", "opened_at": "2026-07-15T12:00:00Z"},
	})
	if got := FindActiveSessionID(&cwd); got != "only" {
		t.Fatalf("got %q", got)
	}
}

func TestFindActiveSessionID_NoMatch(t *testing.T) {
	home := withTempGrokHome(t)
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	if got := FindActiveSessionID(&cwd); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFindLatestSessionIDUnderCwd(t *testing.T) {
	home := withTempGrokHome(t)
	writeSession(t, home, "a", map[string]any{"contextTokensUsed": 1}, time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
	writeSession(t, home, "b", map[string]any{"contextTokensUsed": 2}, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	cwd := testCwd
	if got := FindLatestSessionIDUnderCwd(&cwd); got != "b" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSignalsPath_Direct(t *testing.T) {
	home := withTempGrokHome(t)
	sid := "019f6555-e217-7671-9679-7d72d0aba6ba"
	path := writeSession(t, home, sid, map[string]any{"contextTokensUsed": 100}, time.Now())
	cwd := testCwd
	if got := ResolveSignalsPath(&sid, &cwd); got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestResolveSignalsPath_ViaActive(t *testing.T) {
	home := withTempGrokHome(t)
	path := writeSession(t, home, "active-sid", map[string]any{"contextTokensUsed": 50}, time.Now())
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "active-sid", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	if got := ResolveSignalsPath(nil, &cwd); got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestFindActiveSessionID_TrailingSlash(t *testing.T) {
	home := withTempGrokHome(t)
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "sid", "cwd": testCwd, "opened_at": "2026-07-15T10:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd + "/"
	if got := FindActiveSessionID(&cwd); got != "sid" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSignalsPath_BasenameAfterRename(t *testing.T) {
	home := withTempGrokHome(t)
	// Sessions stored under old cwd leaf; pane reports renamed parent path.
	oldCwd := "/Users/example/workspace/my-app"
	newCwd := "/Users/example/archive/my-app"
	dir := filepath.Join(home, "sessions", encodeCwd(oldCwd), "sess-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "signals.json")
	if err := os.WriteFile(path, []byte(`{"contextTokensUsed":10,"contextWindowTokens":100}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "sess-1", "cwd": oldCwd, "opened_at": "2026-07-16T00:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveSignalsPath(nil, &newCwd); got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestResolveSignalsPath_FiveHistoricalOneLive(t *testing.T) {
	home := withTempGrokHome(t)
	old := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"h1", "h2", "h3", "h4", "h5"} {
		writeSession(t, home, id, signals(i+1), old.Add(time.Duration(i)*time.Hour))
	}
	// Historical h5 is newest on disk; the live session is older on disk.
	live := writeSession(t, home, "live", signals(99), old.Add(-time.Hour))
	writeActiveSessions(t, home, []map[string]any{
		{"session_id": "live", "cwd": testCwd, "opened_at": "2026-07-20T00:00:00Z"},
	})
	cwd := testCwd
	if got := ResolveSignalsPath(nil, &cwd); got != live {
		t.Fatalf("unbound got %q want live %q", got, live)
	}
}

func TestResolveSignalsPath_MultipleLiveUnbound(t *testing.T) {
	home := withTempGrokHome(t)
	writeSession(t, home, "a", signals(10), time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
	writeSession(t, home, "b", signals(20), time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	writeActiveSessions(t, home, []map[string]any{
		{"session_id": "a", "cwd": testCwd, "opened_at": "2026-07-14T00:00:00Z"},
		{"session_id": "b", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
	})
	cwd := testCwd
	if got := ResolveSignalsPath(nil, &cwd); got != "" {
		t.Fatalf("got %q, want unresolved", got)
	}
}

func TestResolveSignalsPath_StaleBindRecoversUniqueLive(t *testing.T) {
	home := withTempGrokHome(t)
	stale := writeSession(t, home, "stale", signals(1684), time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
	live := writeSession(t, home, "live", signals(29000), time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	writeSession(t, home, "h1", signals(1), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	writeSession(t, home, "h2", signals(2), time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC))
	writeActiveSessions(t, home, []map[string]any{
		{"session_id": "live", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
	})
	cwd := testCwd
	staleID := "stale"
	if got := ResolveSignalsPath(&staleID, &cwd); got != live {
		t.Fatalf("got %q want live %q (stale was %q)", got, live, stale)
	}
}

func TestResolveSignalsPath_StaleBindMultipleLiveKeepsBound(t *testing.T) {
	home := withTempGrokHome(t)
	stale := writeSession(t, home, "stale", signals(10), time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
	writeSession(t, home, "a", signals(20), time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	writeSession(t, home, "b", signals(30), time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC))
	writeActiveSessions(t, home, []map[string]any{
		{"session_id": "a", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
		{"session_id": "b", "cwd": testCwd, "opened_at": "2026-07-16T00:00:00Z"},
	})
	cwd := testCwd
	staleID := "stale"
	if got := ResolveSignalsPath(&staleID, &cwd); got != stale {
		t.Fatalf("got %q want bound %q", got, stale)
	}
}

func TestResolveSignalsPath_BoundStillLiveAmongMultiple(t *testing.T) {
	home := withTempGrokHome(t)
	a := writeSession(t, home, "a", signals(10), time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
	writeSession(t, home, "b", signals(20), time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	writeActiveSessions(t, home, []map[string]any{
		{"session_id": "a", "cwd": testCwd, "opened_at": "2026-07-14T00:00:00Z"},
		{"session_id": "b", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
	})
	cwd := testCwd
	id := "a"
	if got := ResolveSignalsPath(&id, &cwd); got != a {
		t.Fatalf("got %q want bound live %q", got, a)
	}
}

func TestResolveSignalsPath_UnboundHistoricalWhenNoLive(t *testing.T) {
	home := withTempGrokHome(t)
	writeSession(t, home, "h1", signals(1), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	newest := writeSession(t, home, "h2", signals(2), time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC))
	writeActiveSessions(t, home, nil)
	cwd := testCwd
	if got := ResolveSignalsPath(nil, &cwd); got != newest {
		t.Fatalf("got %q want newest historical %q", got, newest)
	}
}

func TestResolveSignalsPathIn_StaleBindRecoversUniqueLive(t *testing.T) {
	home := withTempGrokHome(t)
	staleID := "stale"
	writeSession(t, home, staleID, signals(1684), time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
	live := writeSession(t, home, "live", signals(29000), time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	writeActiveSessions(t, home, []map[string]any{
		{"session_id": "live", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
	})
	cwd := testCwd
	if got := ResolveSignalsPathIn(home, &staleID, &cwd); got != live {
		t.Fatalf("got %q want live %q", got, live)
	}
}
