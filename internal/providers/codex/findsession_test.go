/**
 * Tests for FindSessionFile (uses a temporary CODEX_HOME).
 */
package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withTempCodexHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	return dir
}

func writeRollout(t *testing.T, home, sessionID, dayRel string, mtime time.Time) string {
	t.Helper()
	dayPath := filepath.Join(home, "sessions", dayRel)
	if err := os.MkdirAll(dayPath, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dayPath, "rollout-2026-07-12T11-12-23-"+sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session_meta"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeRolloutWithCwd(t *testing.T, home, sessionID, dayRel, cwd string, mtime time.Time, fatMeta bool) string {
	t.Helper()
	dayPath := filepath.Join(home, "sessions", dayRel)
	if err := os.MkdirAll(dayPath, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dayPath, "rollout-2026-07-12T11-12-23-"+sessionID+".jsonl")
	payload := map[string]any{"session_id": sessionID, "cwd": cwd}
	if fatMeta {
		payload["base_instructions"] = map[string]any{"text": strings.Repeat("x", 32*1024)}
	}
	meta, err := json.Marshal(map[string]any{"type": "session_meta", "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(meta, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindSessionFile_ByID(t *testing.T) {
	home := withTempCodexHome(t)
	sessionID := "019f5418-dda2-7cc1-bb2a-3f6051591b8d"
	expected := writeRollout(t, home, sessionID, "2026/07/12", time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC))
	if got := FindSessionFile(sessionID); got != expected {
		t.Fatalf("got %q want %q", got, expected)
	}
}

func TestFindSessionFile_Unknown(t *testing.T) {
	home := withTempCodexHome(t)
	_ = os.MkdirAll(filepath.Join(home, "sessions", "2026", "07", "12"), 0o755)
	if got := FindSessionFile("00000000-0000-0000-0000-000000000000"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFindSessionFile_PrefersNewer(t *testing.T) {
	home := withTempCodexHome(t)
	sessionID := "019f5418-dda2-7cc1-bb2a-3f6051591b8d"
	_ = writeRollout(t, home, sessionID, "2026/07/11", time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC))
	newer := writeRollout(t, home, sessionID, "2026/07/12", time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC))
	if got := FindSessionFile(sessionID); got != newer {
		t.Fatalf("got %q want %q", got, newer)
	}
}

func TestFindLatestSessionFileForCwd(t *testing.T) {
	home := withTempCodexHome(t)
	target := "/Users/example/project"
	_ = writeRolloutWithCwd(t, home, "019f5418-dda2-7cc1-bb2a-3f6051591b8d", "2026/07/11", target, time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC), false)
	newer := writeRolloutWithCwd(t, home, "019f656b-0657-78e2-b16f-9ed6b087283a", "2026/07/15", target, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), false)
	_ = writeRolloutWithCwd(t, home, "019f9999-0000-0000-0000-000000000001", "2026/07/15", "/other/project", time.Date(2026, 7, 15, 13, 0, 0, 0, time.UTC), false)
	if got := FindLatestSessionFileForCwd(target); got != newer {
		t.Fatalf("got %q want %q", got, newer)
	}
}

func TestFindLatestSessionFileForCwd_NoMatch(t *testing.T) {
	home := withTempCodexHome(t)
	_ = writeRolloutWithCwd(t, home, "019f5418-dda2-7cc1-bb2a-3f6051591b8d", "2026/07/12", "/other/project", time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC), false)
	if got := FindLatestSessionFileForCwd("/Users/me/app"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFindLatestSessionFileForCwd_Empty(t *testing.T) {
	withTempCodexHome(t)
	if got := FindLatestSessionFileForCwd(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFindLatestSessionFileForCwd_FatMeta(t *testing.T) {
	home := withTempCodexHome(t)
	target := "/Users/senna/fat-meta"
	path := writeRolloutWithCwd(t, home, "019f656b-0657-78e2-b16f-9ed6b087283a", "2026/07/15", target, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), true)
	if got := FindLatestSessionFileForCwd(target); got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestResolveSessionFile(t *testing.T) {
	home := withTempCodexHome(t)
	target := "/Users/senna/repo"
	byID := writeRolloutWithCwd(t, home, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "2026/07/10", target, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC), false)
	_ = writeRolloutWithCwd(t, home, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "2026/07/15", target, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), false)
	id := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	if got := ResolveSessionFile(&id, &target); got != byID {
		t.Fatalf("prefer id: got %q want %q", got, byID)
	}
	missing := "00000000-0000-0000-0000-000000000000"
	latest := writeRolloutWithCwd(t, home, "cccccccc-cccc-cccc-cccc-cccccccccccc", "2026/07/16", target, time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC), false)
	if got := ResolveSessionFile(&missing, &target); got != latest {
		t.Fatalf("fallback cwd: got %q want %q", got, latest)
	}
	if got := ResolveSessionFile(nil, nil); got != "" {
		t.Fatalf("both nil: got %q", got)
	}
}

func TestFindLatestSessionFileForCwd_TrailingSlash(t *testing.T) {
	home := withTempCodexHome(t)
	target := "/Users/example/project"
	path := writeRolloutWithCwd(t, home, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "2026/07/16", target, time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC), false)
	if got := FindLatestSessionFileForCwd(target + "/"); got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestFindLatestSessionFileForCwd_BasenameAfterRename(t *testing.T) {
	home := withTempCodexHome(t)
	// Session recorded under the old folder name; pane now has a renamed path
	// that keeps the same leaf name (common after project renames).
	oldCwd := "/Users/example/workspace/my-app"
	newCwd := "/Users/example/archive/my-app"
	path := writeRolloutWithCwd(t, home, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "2026/07/16", oldCwd, time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC), false)
	if got := FindLatestSessionFileForCwd(newCwd); got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestFindSessionFileByMetaID_ThreadVsRoot(t *testing.T) {
	home := withTempCodexHome(t)
	// Filename uses thread id; session_meta.session_id is the root session.
	dayPath := filepath.Join(home, "sessions", "2026", "07", "16")
	if err := os.MkdirAll(dayPath, 0o755); err != nil {
		t.Fatal(err)
	}
	threadID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	rootID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	path := filepath.Join(dayPath, "rollout-2026-07-16T12-00-00-"+threadID+".jsonl")
	meta, _ := json.Marshal(map[string]any{
		"type": "session_meta",
		"payload": map[string]any{
			"session_id": rootID,
			"id":         threadID,
			"cwd":        "/Users/example/project",
		},
	})
	if err := os.WriteFile(path, append(meta, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FindSessionFileByMetaID(rootID); got != path {
		t.Fatalf("by session_id: got %q want %q", got, path)
	}
	if got := ResolveSessionFile(&rootID, nil); got != path {
		t.Fatalf("resolve: got %q want %q", got, path)
	}
}
