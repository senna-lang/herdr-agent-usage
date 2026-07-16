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

func TestFindActiveSessionID(t *testing.T) {
	home := withTempGrokHome(t)
	b, _ := json.Marshal([]map[string]any{
		{"session_id": "old-id", "cwd": testCwd, "opened_at": "2026-07-14T10:00:00Z"},
		{"session_id": "new-id", "cwd": testCwd, "opened_at": "2026-07-15T10:00:00Z"},
		{"session_id": "other", "cwd": "/other", "opened_at": "2026-07-15T12:00:00Z"},
	})
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := testCwd
	if got := FindActiveSessionID(&cwd); got != "new-id" {
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
