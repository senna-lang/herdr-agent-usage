/**
 * Tests for BuildClaudePaneProviderResolver and the claude-profile dispatch in
 * TokensForPaneDefault / TotalTokensForProviderDefault.
 */
package limits

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
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

func TestBuildCodexPaneProviderResolver_SingleProfileShortCircuits(t *testing.T) {
	profiles := []codex.CodexProfile{{ID: "codex", Home: t.TempDir()}}
	resolve := BuildCodexPaneProviderResolver(profiles)
	id, ok := resolve(OpenPaneSnapshot{Agent: "codex"})
	if !ok || id != "codex" {
		t.Fatalf("ok=%v id=%q", ok, id)
	}
}

func TestBuildCodexPaneProviderResolver_SingleCustomIDProfile(t *testing.T) {
	profiles := []codex.CodexProfile{{ID: "dev", Home: t.TempDir()}}
	resolve := BuildCodexPaneProviderResolver(profiles)
	id, ok := resolve(OpenPaneSnapshot{Agent: "codex"})
	if !ok || id != "dev" {
		t.Fatalf("ok=%v id=%q, want dev", ok, id)
	}
}

func TestBuildCodexPaneProviderResolver_MultiProfileMatchesBySession(t *testing.T) {
	homeA := t.TempDir()
	homeB := t.TempDir()
	writeCodexRollout(t, homeB, "sess-b")

	profiles := []codex.CodexProfile{
		{ID: "codex", Home: homeA},
		{ID: "dev", Home: homeB},
	}
	resolve := BuildCodexPaneProviderResolver(profiles)
	sid := "sess-b"
	id, ok := resolve(OpenPaneSnapshot{Agent: "codex", SessionID: &sid})
	if !ok || id != "dev" {
		t.Fatalf("ok=%v id=%q", ok, id)
	}
}

func TestBuildCodexPaneProviderResolver_MultiProfileUnknownSessionRefuses(t *testing.T) {
	profiles := []codex.CodexProfile{
		{ID: "codex", Home: t.TempDir()},
		{ID: "dev", Home: t.TempDir()},
	}
	resolve := BuildCodexPaneProviderResolver(profiles)
	sid := "unknown-session"
	_, ok := resolve(OpenPaneSnapshot{Agent: "codex", SessionID: &sid})
	if ok {
		t.Fatal("unresolved codex session must not attribute to any profile")
	}
}

func writeCodexRollout(t *testing.T, home, sessionID string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "07", "12")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-07-12T11-12-23-"+sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session_meta"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
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

func writeGrokSignals(t *testing.T, home, cwd, sessionID string) {
	t.Helper()
	encodedCwd := strings.ReplaceAll(url.QueryEscape(cwd), "+", "%20")
	dir := filepath.Join(home, "sessions", encodedCwd, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "signals.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildGrokPaneProviderResolver_MultiProfileUsesOwnSessionStore(t *testing.T) {
	homeA := t.TempDir()
	homeB := t.TempDir()
	cwd := "/work/project"
	writeGrokSignals(t, homeB, cwd, "session-b")

	resolve := BuildGrokPaneProviderResolver([]grok.GrokProfile{
		{ID: "grok-personal", Home: homeA},
		{ID: "grok-work", Home: homeB},
	})
	sessionID := "session-b"
	got, ok := resolve(OpenPaneSnapshot{Agent: "grok", SessionID: &sessionID})
	if !ok || got != "grok-work" {
		t.Fatalf("ok=%v provider=%q", ok, got)
	}
}

func TestBuildGrokPaneProviderResolver_MultiProfileRejectsAmbiguousAndUnknown(t *testing.T) {
	homeA := t.TempDir()
	homeB := t.TempDir()
	cwd := "/work/project"
	writeGrokSignals(t, homeA, cwd, "shared")
	writeGrokSignals(t, homeB, cwd, "shared")

	resolve := BuildGrokPaneProviderResolver([]grok.GrokProfile{
		{ID: "grok-personal", Home: homeA},
		{ID: "grok-work", Home: homeB},
	})
	shared := "shared"
	if _, ok := resolve(OpenPaneSnapshot{Agent: "grok", SessionID: &shared}); ok {
		t.Fatal("a session in multiple profile homes must be rejected")
	}
	unknown := "unknown"
	if _, ok := resolve(OpenPaneSnapshot{Agent: "grok", SessionID: &unknown}); ok {
		t.Fatal("an unknown session must be rejected")
	}
}

func writeOpenCodeSession(t *testing.T, dataDir, sessionID, cwd string) {
	t.Helper()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT, time_archived INTEGER, time_updated INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session (id, directory, time_archived, time_updated) VALUES (?, ?, NULL, 1)`, sessionID, cwd); err != nil {
		t.Fatal(err)
	}
}

func writeOpenCodeUsage(t *testing.T, dataDir, sessionID string, tokens int) {
	t.Helper()
	dbPath := filepath.Join(dataDir, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, data TEXT, time_created INTEGER)`); err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf(`{"role":"assistant","providerID":"opencode-go","tokens":{"input":%d},"time":{"created":100}}`, tokens)
	if _, err := db.Exec(`INSERT INTO message (id, session_id, data, time_created) VALUES (?, ?, ?, 100)`, "message-"+sessionID, sessionID, data); err != nil {
		t.Fatal(err)
	}
}

func TestBuildOpenCodePaneProviderResolver_MultiProfileUsesOwnSessionStore(t *testing.T) {
	dataDirA := t.TempDir()
	dataDirB := t.TempDir()
	writeOpenCodeSession(t, dataDirB, "session-b", "/work/project")

	resolve := BuildOpenCodePaneProviderResolver([]opencode.OpenCodeProfile{
		{ID: "opencode-personal", DataDir: dataDirA},
		{ID: "opencode-work", DataDir: dataDirB},
	})
	sessionID := "session-b"
	got, ok := resolve(OpenPaneSnapshot{Agent: "opencode", SessionID: &sessionID})
	if !ok || got != "opencode-work" {
		t.Fatalf("ok=%v provider=%q", ok, got)
	}
}

func TestBuildOpenCodePaneProviderResolver_MultiProfileRejectsAmbiguousAndUnknown(t *testing.T) {
	dataDirA := t.TempDir()
	dataDirB := t.TempDir()
	writeOpenCodeSession(t, dataDirA, "shared", "/work/project")
	writeOpenCodeSession(t, dataDirB, "shared", "/work/project")

	resolve := BuildOpenCodePaneProviderResolver([]opencode.OpenCodeProfile{
		{ID: "opencode-personal", DataDir: dataDirA},
		{ID: "opencode-work", DataDir: dataDirB},
	})
	shared := "shared"
	if _, ok := resolve(OpenPaneSnapshot{Agent: "opencode", SessionID: &shared}); ok {
		t.Fatal("a session in multiple profile databases must be rejected")
	}
	unknown := "unknown"
	if _, ok := resolve(OpenPaneSnapshot{Agent: "opencode", SessionID: &unknown}); ok {
		t.Fatal("an unknown session must be rejected")
	}
}

func TestBuildOpenCodePaneProviderResolver_MultiProfileRejectsUnknownSession(t *testing.T) {
	dataDirA := t.TempDir()
	dataDirB := t.TempDir()
	writeOpenCodeSession(t, dataDirA, "known", "/work/project")

	resolve := BuildOpenCodePaneProviderResolver([]opencode.OpenCodeProfile{
		{ID: "opencode-personal", DataDir: dataDirA},
		{ID: "opencode-work", DataDir: dataDirB},
	})
	unknown := "unknown"
	if _, ok := resolve(OpenPaneSnapshot{Agent: "opencode", SessionID: &unknown}); ok {
		t.Fatal("an unknown session must not be attributed to the only database with sessions")
	}
}

func TestBuildPaneActivityProviderResolver_OpenCodeProfilesIgnoreAmbientRoute(t *testing.T) {
	personalDir := t.TempDir()
	workDir := t.TempDir()
	ambientDir := t.TempDir()
	const sessionID = "session-work"
	const cwd = "/work/project"

	writeOpenCodeSession(t, workDir, sessionID, cwd)
	writeOpenCodeSession(t, ambientDir, sessionID, cwd)
	writeOpenCodeUsage(t, ambientDir, sessionID, 10)
	t.Setenv("OPENCODE_DB", filepath.Join(ambientDir, "opencode.db"))

	resolve := BuildPaneActivityProviderResolver(
		nil,
		nil,
		nil,
		[]opencode.OpenCodeProfile{
			{ID: "opencode-personal", DataDir: personalDir},
			{ID: "opencode-work", DataDir: workDir},
		},
	)
	got, ok := resolve(OpenPaneSnapshot{Agent: "opencode", SessionID: strPtr(sessionID)})
	if !ok || got != "opencode-work" {
		t.Fatalf("ok=%v provider=%q, want opencode-work", ok, got)
	}
}

func TestTokensForPaneDefault_OpenCodeProfileReadsOnlyItsDatabase(t *testing.T) {
	pluginConfigDir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir)
	dataDirA := t.TempDir()
	dataDirB := t.TempDir()
	writeOpenCodeSession(t, dataDirA, "shared", "/work/project")
	writeOpenCodeSession(t, dataDirB, "shared", "/work/project")
	writeOpenCodeUsage(t, dataDirA, "shared", 10)
	writeOpenCodeUsage(t, dataDirB, "shared", 30)

	config := "[[opencode.profiles]]\n" +
		"id = \"opencode-personal\"\n" +
		"data_dir = \"" + dataDirA + "\"\n\n" +
		"[[opencode.profiles]]\n" +
		"id = \"opencode-work\"\n" +
		"data_dir = \"" + dataDirB + "\"\n"
	if err := os.WriteFile(filepath.Join(pluginConfigDir, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	sessionID := "shared"
	pane := OpenPaneSnapshot{Agent: "opencode", SessionID: &sessionID}
	if got := TokensForPaneDefault("opencode-personal", pane, 0, 1_000); got != 10 {
		t.Fatalf("personal tokens = %v, want 10", got)
	}
	if got := TokensForPaneDefault("opencode-work", pane, 0, 1_000); got != 30 {
		t.Fatalf("work tokens = %v, want 30", got)
	}
}

func TestTokensForPaneDefault_GrokProfileReadsOnlyItsHome(t *testing.T) {
	pluginConfigDir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir)
	homeA := t.TempDir()
	homeB := t.TempDir()
	cwd := "/work/project"
	writeGrokSignals(t, homeA, cwd, "shared")
	writeGrokSignals(t, homeB, cwd, "shared")
	encodedCwd := strings.ReplaceAll(url.QueryEscape(cwd), "+", "%20")
	writeUpdates := func(home string, tokens int) {
		path := filepath.Join(home, "sessions", encodedCwd, "shared", "updates.jsonl")
		line := fmt.Sprintf(`{"timestamp":0.1,"params":{"update":{"sessionUpdate":"turn_completed","usage":{"totalTokens":%d}}}}`, tokens)
		if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeUpdates(homeA, 10)
	writeUpdates(homeB, 30)

	config := "[[grok.profiles]]\n" +
		"id = \"grok-personal\"\n" +
		"grok_home = \"" + homeA + "\"\n\n" +
		"[[grok.profiles]]\n" +
		"id = \"grok-work\"\n" +
		"grok_home = \"" + homeB + "\"\n"
	if err := os.WriteFile(filepath.Join(pluginConfigDir, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	sessionID := "shared"
	pane := OpenPaneSnapshot{Agent: "grok", SessionID: &sessionID}
	if got := TokensForPaneDefault("grok-personal", pane, 0, 1_000); got != 10 {
		t.Fatalf("personal tokens = %v, want 10", got)
	}
	if got := TokensForPaneDefault("grok-work", pane, 0, 1_000); got != 30 {
		t.Fatalf("work tokens = %v, want 30", got)
	}
}
