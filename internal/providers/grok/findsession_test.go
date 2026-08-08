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

func TestFindActiveSessionID_UniqueOnly(t *testing.T) {
	home := withTempGrokHome(t)
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "only-id", "cwd": testCwd, "opened_at": "2026-07-15T10:00:00Z"},
		{"session_id": "other", "cwd": "/other", "opened_at": "2026-07-15T12:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	if got := FindActiveSessionID(&cwd); got != "only-id" {
		t.Fatalf("got %q", got)
	}
}

func TestFindActiveSessionID_AmbiguousSameCwd(t *testing.T) {
	home := withTempGrokHome(t)
	// Two panes under $HOME / same project: never pick "most recent".
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "old-id", "cwd": testCwd, "opened_at": "2026-07-14T10:00:00Z"},
		{"session_id": "new-id", "cwd": testCwd, "opened_at": "2026-07-15T10:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	if got := FindActiveSessionID(&cwd); got != "" {
		t.Fatalf("ambiguous active sessions must not guess, got %q", got)
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

func TestFindLatestSessionIDUnderCwd_UniqueOnly(t *testing.T) {
	home := withTempGrokHome(t)
	writeSession(t, home, "only", map[string]any{"contextTokensUsed": 1}, time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
	cwd := testCwd
	if got := FindLatestSessionIDUnderCwd(&cwd); got != "only" {
		t.Fatalf("got %q", got)
	}
}

func TestFindLatestSessionIDUnderCwd_AmbiguousSkipsNewest(t *testing.T) {
	home := withTempGrokHome(t)
	// Newer busy sibling must not be chosen when the group has multiple sessions.
	writeSession(t, home, "a", map[string]any{"contextTokensUsed": 23_000}, time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
	writeSession(t, home, "b", map[string]any{"contextTokensUsed": 146_000}, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	cwd := testCwd
	if got := FindLatestSessionIDUnderCwd(&cwd); got != "" {
		t.Fatalf("multi-session cwd must not pick newest, got %q", got)
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

func TestResolveSignalsPath_ViaUniqueOnDisk(t *testing.T) {
	home := withTempGrokHome(t)
	// Without agent_session, only a unique on-disk session is trusted.
	// active_sessions alone is not enough (incomplete under multi-pane $HOME).
	path := writeSession(t, home, "only-sid", map[string]any{"contextTokensUsed": 50}, time.Now())
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "only-sid", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
		// A second active entry would previously have blocked; on-disk is still unique.
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	if got := ResolveSignalsPath(nil, &cwd); got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestResolveSessionDir_ActiveAloneNotEnoughWhenDiskMulti(t *testing.T) {
	home := withTempGrokHome(t)
	writeSession(t, home, "a", map[string]any{"contextTokensUsed": 10}, time.Now())
	writeSession(t, home, "b", map[string]any{"contextTokensUsed": 200_000}, time.Now())
	b, _ := json.Marshal([]map[string]any{
		// Only one active — historically wrong for multi-pane same cwd.
		{"session_id": "b", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	if got := ResolveSessionDir(nil, &cwd); got != "" {
		t.Fatalf("must not use sole active when disk has multiple sessions, got %q", got)
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
	// With a bound session id this still resolves; without id, basename weak
	// match only works when the group is unique (single session).
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
	sid := "sess-1"
	if got := ResolveSignalsPath(&sid, &newCwd); got != path {
		t.Fatalf("bound id: got %q want %q", got, path)
	}
	// Unbound: unique session under same-project group still resolves.
	if got := ResolveSignalsPath(nil, &newCwd); got != path {
		t.Fatalf("unique weak cwd: got %q want %q", got, path)
	}
}

func TestResolveSessionDir_StaleBindRecoversToUniqueActive(t *testing.T) {
	home := withTempGrokHome(t)
	// Herdr still holds the session_start id (48k); real active session is 216k.
	stale := writeSession(t, home, "stale-48k", map[string]any{"contextTokensUsed": 48_000}, time.Now())
	activePath := writeSession(t, home, "active-216k", map[string]any{"contextTokensUsed": 216_000}, time.Now())
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "active-216k", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	sid := "stale-48k"
	gotDir := ResolveSessionDir(&sid, &cwd)
	wantDir := filepath.Dir(activePath)
	if gotDir != wantDir {
		t.Fatalf("stale bind should recover to unique active, got %q want %q (stale was %q)", gotDir, wantDir, filepath.Dir(stale))
	}
	if got := ResolveSignalsPath(&sid, &cwd); got != activePath {
		t.Fatalf("signals path: got %q want %q", got, activePath)
	}
	u := ResolveUsageForGrok(&sid, &cwd)
	if u == nil || u.ContextTokens != 216_000 {
		t.Fatalf("usage: got %+v want 216k", u)
	}
}

func TestResolveSessionDir_BoundStillActiveKeepsBound(t *testing.T) {
	home := withTempGrokHome(t)
	// Multi-pane same cwd: each bound id is active — never steal sibling.
	boundPath := writeSession(t, home, "pane-a-23k", map[string]any{"contextTokensUsed": 23_000}, time.Now())
	writeSession(t, home, "pane-b-146k", map[string]any{"contextTokensUsed": 146_000}, time.Now())
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "pane-a-23k", "cwd": testCwd, "opened_at": "2026-07-14T00:00:00Z"},
		{"session_id": "pane-b-146k", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	sid := "pane-a-23k"
	if got := ResolveSignalsPath(&sid, &cwd); got != boundPath {
		t.Fatalf("bound still active must keep bound, got %q want %q", got, boundPath)
	}
}

func TestResolveSessionDir_StaleBindNoUniqueActiveKeepsBound(t *testing.T) {
	home := withTempGrokHome(t)
	// Stale bound + ambiguous actives → stick with bound (no sibling guess).
	boundPath := writeSession(t, home, "stale-bound", map[string]any{"contextTokensUsed": 10_000}, time.Now())
	writeSession(t, home, "active-a", map[string]any{"contextTokensUsed": 100_000}, time.Now())
	writeSession(t, home, "active-b", map[string]any{"contextTokensUsed": 200_000}, time.Now())
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "active-a", "cwd": testCwd, "opened_at": "2026-07-14T00:00:00Z"},
		{"session_id": "active-b", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	sid := "stale-bound"
	if got := ResolveSignalsPath(&sid, &cwd); got != boundPath {
		t.Fatalf("no unique active: keep bound, got %q want %q", got, boundPath)
	}
}

func TestResolveSessionDir_MissingBoundRecoversToUniqueActive(t *testing.T) {
	home := withTempGrokHome(t)
	// Bound id missing on disk + unique active → recover (stale bind path).
	activePath := writeSession(t, home, "sibling-146k", map[string]any{"contextTokensUsed": 146_000}, time.Now())
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "sibling-146k", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	missing := "missing-bound-id"
	if got := ResolveSignalsPath(&missing, &cwd); got != activePath {
		t.Fatalf("stale missing bound + unique active should recover, got %q want %q", got, activePath)
	}
}

func TestResolveSessionDir_MissingBoundAmbiguousActiveStaysEmpty(t *testing.T) {
	home := withTempGrokHome(t)
	writeSession(t, home, "a", map[string]any{"contextTokensUsed": 10}, time.Now())
	writeSession(t, home, "b", map[string]any{"contextTokensUsed": 200_000}, time.Now())
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "a", "cwd": testCwd, "opened_at": "2026-07-14T00:00:00Z"},
		{"session_id": "b", "cwd": testCwd, "opened_at": "2026-07-15T00:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	missing := "missing-bound-id"
	if dir := ResolveSessionDir(&missing, &cwd); dir != "" {
		t.Fatalf("missing bound + ambiguous active must stay empty, got %q", dir)
	}
}

func TestResolveSessionDir_BoundWithoutSignals(t *testing.T) {
	home := withTempGrokHome(t)
	sid := "no-signals-yet"
	dir := filepath.Join(home, "sessions", encodeCwd(testCwd), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only updates.jsonl — still a valid session dir.
	if err := os.WriteFile(filepath.Join(dir, "updates.jsonl"), []byte(`{"params":{"_meta":{"totalTokens":9000}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	if got := ResolveSessionDir(&sid, &cwd); got != dir {
		t.Fatalf("got %q want %q", got, dir)
	}
	// signals path empty, but usage still resolves via updates.
	if ResolveSignalsPath(&sid, &cwd) != "" {
		t.Fatal("expected no signals path")
	}
	u := ResolveUsageForGrok(&sid, &cwd)
	if u == nil || u.ContextTokens != 9000 {
		t.Fatalf("got %+v", u)
	}
}

func TestResolveSignalsPath_BoundSessionBeatsNewerSibling(t *testing.T) {
	home := withTempGrokHome(t)
	old := writeSession(t, home, "brew-23k", map[string]any{"contextTokensUsed": 23_000}, time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
	writeSession(t, home, "coding-146k", map[string]any{"contextTokensUsed": 146_000}, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	cwd := testCwd
	sid := "brew-23k"
	if got := ResolveSignalsPath(&sid, &cwd); got != old {
		t.Fatalf("got %q want brew session %q", got, old)
	}
}
