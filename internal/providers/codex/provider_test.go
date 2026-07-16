/**
 * Tests for codexProvider's session-kind interpretation and cwd fallback.
 */
package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

func writeUsageRollout(t *testing.T, home, sessionID, dayRel, cwd string, mtime time.Time, contextTokens, windowTokens int) string {
	t.Helper()
	dayPath := filepath.Join(home, "sessions", dayRel)
	if err := os.MkdirAll(dayPath, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dayPath, "rollout-2026-07-15T19-55-40-"+sessionID+".jsonl")
	meta, _ := json.Marshal(map[string]any{
		"type": "session_meta", "payload": map[string]any{"session_id": sessionID, "cwd": cwd},
	})
	token, _ := json.Marshal(map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{
				"last_token_usage":     map[string]any{"total_tokens": contextTokens},
				"model_context_window": windowTokens,
			},
		},
	})
	content := string(meta) + "\n" + string(token) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProvider_AgentID(t *testing.T) {
	if Provider.AgentID() != "codex" {
		t.Fatalf("got %q", Provider.AgentID())
	}
}

func TestProvider_NullCases(t *testing.T) {
	withTempCodexHome(t)
	if Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "path", Value: "/tmp/x"},
	}) != nil {
		t.Fatal("expected nil")
	}
	if Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: ""},
	}) != nil {
		t.Fatal("expected nil")
	}
	if Provider.ResolveUsage(provider.UsageResolveInput{}) != nil {
		t.Fatal("expected nil")
	}
	if Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: "00000000-0000-0000-0000-000000000000"},
	}) != nil {
		t.Fatal("expected nil for unknown")
	}
}

func TestProvider_CwdFallbackWrongID(t *testing.T) {
	home := withTempCodexHome(t)
	cwd := "/Users/example/project"
	writeUsageRollout(t, home, "019f656b-0657-78e2-b16f-9ed6b087283a", "2026/07/15", cwd, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), 17_377, 258_400)
	got := Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: "019f656c-6508-7533-b46a-4b31bca88013"},
		Cwd:     &cwd,
	})
	if got == nil || got.ContextTokens != 17_377 || got.WindowTokens == nil || *got.WindowTokens != 258_400 {
		t.Fatalf("got %+v", got)
	}
}

func TestProvider_CwdOnly(t *testing.T) {
	home := withTempCodexHome(t)
	cwd := "/Users/senna/repo"
	writeUsageRollout(t, home, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "2026/07/15", cwd, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), 1000, 200_000)
	got := Provider.ResolveUsage(provider.UsageResolveInput{Cwd: &cwd})
	if got == nil || got.ContextTokens != 1000 || got.WindowTokens == nil || *got.WindowTokens != 200_000 {
		t.Fatalf("got %+v", got)
	}
}
