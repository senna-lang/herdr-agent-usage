/**
 * Tests for grokProvider resolution.
 */
package grok

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

const provCwd = "/tmp/usagebar-grok-proj"

func TestProvider_AgentID(t *testing.T) {
	if Provider.AgentID() != "grok" {
		t.Fatalf("got %q", Provider.AgentID())
	}
}

func TestProvider_FromSessionID(t *testing.T) {
	home := withTempGrokHome(t)
	sid := "019f6555-e217-7671-9679-7d72d0aba6ba"
	dir := filepath.Join(home, "sessions", encodeCwd(provCwd), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"contextTokensUsed": 169_111, "contextWindowTokens": 500_000})
	if err := os.WriteFile(filepath.Join(dir, "signals.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := provCwd
	got := Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: sid},
		Cwd:     &cwd,
	})
	if got == nil || got.ContextTokens != 169_111 || got.WindowTokens == nil || *got.WindowTokens != 500_000 {
		t.Fatalf("got %+v", got)
	}
}

func TestProvider_ViaUniqueOnDiskSession(t *testing.T) {
	home := withTempGrokHome(t)
	sid := "only-on-disk"
	dir := filepath.Join(home, "sessions", encodeCwd(provCwd), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"contextTokensUsed": 12_000, "contextWindowTokens": 200_000})
	if err := os.WriteFile(filepath.Join(dir, "signals.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := provCwd
	// No agent_session: unique on-disk session under cwd is enough.
	got := Provider.ResolveUsage(provider.UsageResolveInput{Cwd: &cwd})
	if got == nil || got.ContextTokens != 12_000 || got.WindowTokens == nil || *got.WindowTokens != 200_000 {
		t.Fatalf("got %+v", got)
	}
}

func TestProvider_NothingAvailable(t *testing.T) {
	withTempGrokHome(t)
	if Provider.ResolveUsage(provider.UsageResolveInput{}) != nil {
		t.Fatal("expected nil")
	}
}
