/**
 * Tests for BuildClaudePaneProviderResolver and the claude-profile dispatch in
 * TokensForPaneDefault / TotalTokensForProviderDefault.
 */
package limits

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/claude"
)

func writeTranscript(t *testing.T, root, projectDir, sessionID, body string) {
	t.Helper()
	dir := filepath.Join(root, projectDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildClaudePaneProviderResolver_SingleProfileShortCircuits(t *testing.T) {
	profiles := []claude.ClaudeProfile{{ID: "claude", ProjectsRoot: t.TempDir()}}
	resolve := BuildClaudePaneProviderResolver(profiles)
	id, ok := resolve(OpenPaneSnapshot{Agent: "claude"})
	if !ok || id != "claude" {
		t.Fatalf("ok=%v id=%q", ok, id)
	}
	// Non-claude agents still resolve via the static map.
	id, ok = resolve(OpenPaneSnapshot{Agent: "codex"})
	if !ok || id != "codex" {
		t.Fatalf("codex: ok=%v id=%q", ok, id)
	}
}

func TestBuildClaudePaneProviderResolver_SingleCustomIDProfile(t *testing.T) {
	// A single explicitly configured profile need not be id "claude" —
	// the resolver must still attribute the pane to that profile's id.
	profiles := []claude.ClaudeProfile{{ID: "work", ProjectsRoot: t.TempDir()}}
	resolve := BuildClaudePaneProviderResolver(profiles)
	id, ok := resolve(OpenPaneSnapshot{Agent: "claude"})
	if !ok || id != "work" {
		t.Fatalf("ok=%v id=%q, want work", ok, id)
	}
}

func TestBuildClaudePaneProviderResolver_MultiProfileMatchesBySession(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeTranscript(t, rootB, "-home-b", "sess-b", "line\n")

	profiles := []claude.ClaudeProfile{
		{ID: "claude", ProjectsRoot: rootA},
		{ID: "claude-secondary", ProjectsRoot: rootB},
	}
	resolve := BuildClaudePaneProviderResolver(profiles)
	sid := "sess-b"
	id, ok := resolve(OpenPaneSnapshot{Agent: "claude", SessionID: &sid})
	if !ok || id != "claude-secondary" {
		t.Fatalf("ok=%v id=%q", ok, id)
	}
}

func TestBuildClaudePaneProviderResolver_MultiProfileUnknownSessionRefuses(t *testing.T) {
	profiles := []claude.ClaudeProfile{
		{ID: "claude", ProjectsRoot: t.TempDir()},
		{ID: "claude-secondary", ProjectsRoot: t.TempDir()},
	}
	resolve := BuildClaudePaneProviderResolver(profiles)
	sid := "unknown-session"
	_, ok := resolve(OpenPaneSnapshot{Agent: "claude", SessionID: &sid})
	if ok {
		t.Fatal("unresolved claude session must not attribute to any profile")
	}
}

func TestBuildClaudePaneProviderResolver_MultiProfileNonClaudeUnaffected(t *testing.T) {
	profiles := []claude.ClaudeProfile{
		{ID: "claude", ProjectsRoot: t.TempDir()},
		{ID: "claude-secondary", ProjectsRoot: t.TempDir()},
	}
	resolve := BuildClaudePaneProviderResolver(profiles)
	id, ok := resolve(OpenPaneSnapshot{Agent: "grok"})
	if !ok || id != "grok" {
		t.Fatalf("ok=%v id=%q", ok, id)
	}
}

func claudeUsageLine() string {
	return `{"type":"assistant","isSidechain":false,"timestamp":"2026-01-01T00:00:00.000Z","message":{"model":"claude-sonnet-5","usage":{"input_tokens":100,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":10}}}` + "\n"
}

func TestTokensForPaneDefault_DispatchesToResolvedProfileRoot(t *testing.T) {
	pluginConfigDir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir)
	configDirA := t.TempDir()
	configDirB := t.TempDir()

	cfg := "[[claude.profiles]]\n" +
		"id = \"claude\"\n" +
		"config_dir = \"" + configDirA + "\"\n\n" +
		"[[claude.profiles]]\n" +
		"id = \"claude-secondary\"\n" +
		"config_dir = \"" + configDirB + "\"\n"
	if err := os.WriteFile(filepath.Join(pluginConfigDir, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	// claude-secondary's ProjectsRoot is <configDirB>/projects (see profile.go).
	writeTranscript(t, filepath.Join(configDirB, "projects"), "-home-b", "sess-real", claudeUsageLine())

	sid := "sess-real"
	pane := OpenPaneSnapshot{Agent: "claude", SessionID: &sid}
	tokens := TokensForPaneDefault("claude-secondary", pane, 0, 1<<62)
	if tokens != 110 {
		t.Fatalf("tokens=%v want 110", tokens)
	}
	// The default "claude" profile must not see claude-secondary's session.
	if got := TokensForPaneDefault("claude", pane, 0, 1<<62); got != 0 {
		t.Fatalf("default profile should not see claude-secondary's session, got %v", got)
	}
}
