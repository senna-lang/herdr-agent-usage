/**
 * I/O integration tests for Claude / Grok backend + billing detection.
 *
 * Mirrors the live smoke paths (temp GROK_HOME / CLAUDE_CONFIG_DIR) so a
 * config.toml base_url or settings.json env change surfaces as PaneBackendID
 * and PaneBillingMode without needing a real Bedrock/Ollama call.
 */
package limits

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ioTestCwd = "/tmp/herdr-usage-smoke-cwd"

func encodeGrokCwd(cwd string) string {
	return strings.ReplaceAll(url.QueryEscape(cwd), "+", "%20")
}

// clearClaudeDeployEnv removes process-level deployment flags so tests only
// see the temp settings files they write.
func clearClaudeDeployEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"CLAUDE_CODE_USE_BEDROCK",
		"CLAUDE_CODE_USE_VERTEX",
		"CLAUDE_CODE_USE_FOUNDRY",
		"CLAUDE_CODE_USE_MANTLE",
		"ANTHROPIC_BASE_URL",
		"ANTHROPIC_BEDROCK_BASE_URL",
		"ANTHROPIC_BEDROCK_MANTLE_BASE_URL",
		"ANTHROPIC_AWS_BASE_URL",
		"ANTHROPIC_VERTEX_BASE_URL",
		"ANTHROPIC_FOUNDRY_BASE_URL",
		"GROK_MODELS_BASE_URL",
	} {
		t.Setenv(k, "")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func withTempGrokHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	return home
}

func withTempClaudeConfig(t *testing.T) (configDir, jsonPath string) {
	t.Helper()
	clearClaudeDeployEnv(t)
	configDir = t.TempDir()
	jsonPath = filepath.Join(t.TempDir(), "claude.json")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	t.Setenv("CLAUDE_CONFIG_JSON", jsonPath)
	// Avoid a real statusLine cache promoting PAYG → subscription mid-test.
	t.Setenv("HOME", t.TempDir())
	return configDir, jsonPath
}

func writeGrokSession(t *testing.T, home, sessionID, modelID string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", encodeGrokCwd(ioTestCwd), sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "signals.json"), `{"contextTokensUsed":100,"contextWindowTokens":200000}`)
	writeFile(t, filepath.Join(dir, "summary.json"), `{"current_model_id":`+jsonQuote(modelID)+`}`)
	// updates.jsonl with one usage event so GrokModelIDFromLines can also work.
	writeFile(t, filepath.Join(dir, "updates.jsonl"),
		`{"timestamp":1800000000,"params":{"update":{"sessionUpdate":"turn_completed","modelId":`+jsonQuote(modelID)+`,"usage":{"totalTokens":42}}}}`+"\n")
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestIO_GrokCustomModel_PaneIsPayAsYouGoOllama(t *testing.T) {
	home := withTempGrokHome(t)
	clearClaudeDeployEnv(t)
	writeFile(t, filepath.Join(home, "config.toml"), `
[models]
default = "smoke-ollama"

[model.smoke-ollama]
model = "codellama"
base_url = "http://localhost:11434/v1"
name = "Smoke Ollama"
`)
	// OIDC auth would be subscription at account level; custom model must still win.
	writeFile(t, filepath.Join(home, "auth.json"), `{
		"https://auth.x.ai::test": {"auth_mode": "oidc", "email": "t@example.com"}
	}`)
	sid := "smoke-session-ollama"
	writeGrokSession(t, home, sid, "smoke-ollama")

	cwd := ioTestCwd
	pane := OpenPaneSnapshot{PaneID: "g1", Agent: "grok", SessionID: &sid, Cwd: &cwd}

	if got := GrokBackendForModelID("smoke-ollama"); got != "ollama" {
		t.Fatalf("GrokBackendForModelID: got %q want ollama", got)
	}
	if mode := PaneBillingMode("grok", pane, DefaultBillingDeps()); mode != BillingPayAsYouGo {
		t.Fatalf("PaneBillingMode: got %v want PayAsYouGo", mode)
	}
	if back := PaneBackendID("grok", pane); back != "ollama" {
		t.Fatalf("PaneBackendID: got %q want ollama", back)
	}
	// Subscription filter: only-grok open panes on PAYG → exclude grok.
	set := BillingProviderFilter([]OpenPaneSnapshot{pane}, true, DefaultBillingDeps())
	if set["grok"] {
		t.Fatalf("grok should be excluded from subscription providers: %#v", set)
	}
}

func TestIO_GrokBuiltinModel_SubscriptionKeepsNoBackend(t *testing.T) {
	home := withTempGrokHome(t)
	clearClaudeDeployEnv(t)
	writeFile(t, filepath.Join(home, "config.toml"), `
[models]
default = "grok-4.5"
`)
	writeFile(t, filepath.Join(home, "auth.json"), `{
		"https://auth.x.ai::test": {"auth_mode": "oidc", "email": "t@example.com"}
	}`)
	sid := "smoke-session-xai"
	writeGrokSession(t, home, sid, "grok-4.5")

	cwd := ioTestCwd
	pane := OpenPaneSnapshot{PaneID: "g2", Agent: "grok", SessionID: &sid, Cwd: &cwd}

	if got := GrokBackendForModelID("grok-4.5"); got != "xai" {
		t.Fatalf("GrokBackendForModelID: got %q want xai", got)
	}
	// Pane session is xAI → Unknown at pane; account OIDC → Subscription.
	if mode := PaneBillingMode("grok", pane, DefaultBillingDeps()); mode != BillingSubscription {
		t.Fatalf("PaneBillingMode: got %v want Subscription", mode)
	}
	if back := PaneBackendID("grok", pane); back != "" {
		t.Fatalf("PaneBackendID on subscription must be empty, got %q", back)
	}
	set := BillingProviderFilter([]OpenPaneSnapshot{pane}, true, DefaultBillingDeps())
	if !set["grok"] {
		t.Fatalf("grok subscription pane must keep provider: %#v", set)
	}
}

func TestIO_GrokModelsBaseURLEnvOverride(t *testing.T) {
	home := withTempGrokHome(t)
	clearClaudeDeployEnv(t)
	writeFile(t, filepath.Join(home, "config.toml"), `[models]
default = "grok-4.5"
`)
	t.Setenv("GROK_MODELS_BASE_URL", "https://api.together.xyz/v1")
	if got := GrokBackendForModelID("grok-4.5"); got != "together" {
		t.Fatalf("env override: got %q want together", got)
	}
}

func TestIO_ClaudeBedrockSettings_PaneIsPayAsYouGo(t *testing.T) {
	configDir, jsonPath := withTempClaudeConfig(t)
	// Account still looks like a subscription — Bedrock env must win.
	writeFile(t, jsonPath, `{
		"cachedUsageUtilization": {"utilization": {"five_hour": {"utilization": 10}}},
		"oauthAccount": {"billingType": "stripe_subscription"}
	}`)
	writeFile(t, filepath.Join(configDir, "settings.json"), `{
		"env": {"CLAUDE_CODE_USE_BEDROCK": "1", "AWS_REGION": "us-east-1"}
	}`)

	cwd := ioTestCwd
	pane := OpenPaneSnapshot{PaneID: "c1", Agent: "claude", Cwd: &cwd}

	if got := AccountClaudeBackendID(); got != "bedrock" {
		t.Fatalf("AccountClaudeBackendID: got %q want bedrock", got)
	}
	if mode := PaneBillingMode("claude", pane, DefaultBillingDeps()); mode != BillingPayAsYouGo {
		t.Fatalf("PaneBillingMode: got %v want PayAsYouGo", mode)
	}
	if back := PaneBackendID("claude", pane); back != "bedrock" {
		t.Fatalf("PaneBackendID: got %q want bedrock", back)
	}
	set := BillingProviderFilter([]OpenPaneSnapshot{pane}, true, DefaultBillingDeps())
	if set["claude"] {
		t.Fatalf("claude bedrock pane should be excluded: %#v", set)
	}
}

func TestIO_ClaudeSubscriptionWithoutDeployEnv(t *testing.T) {
	configDir, jsonPath := withTempClaudeConfig(t)
	writeFile(t, jsonPath, `{
		"cachedUsageUtilization": {"utilization": {"five_hour": {"utilization": 10}}},
		"oauthAccount": {"billingType": "stripe_subscription"}
	}`)
	writeFile(t, filepath.Join(configDir, "settings.json"), `{"model":"opus"}`)

	cwd := ioTestCwd
	pane := OpenPaneSnapshot{PaneID: "c2", Agent: "claude", Cwd: &cwd}

	if got := AccountClaudeBackendID(); got != "anthropic" {
		t.Fatalf("fallback backend: got %q", got)
	}
	if mode := PaneBillingMode("claude", pane, DefaultBillingDeps()); mode != BillingSubscription {
		t.Fatalf("PaneBillingMode: got %v want Subscription", mode)
	}
	if back := PaneBackendID("claude", pane); back != "" {
		t.Fatalf("subscription PaneBackendID must be empty, got %q", back)
	}
}

func TestIO_ClaudeProjectSettingsOverrideUser(t *testing.T) {
	configDir, jsonPath := withTempClaudeConfig(t)
	writeFile(t, jsonPath, `{"oauthAccount":{"billingType":"stripe_subscription"},"cachedUsageUtilization":{"utilization":{}}}`)
	// User settings: no cloud flag.
	writeFile(t, filepath.Join(configDir, "settings.json"), `{"env":{}}`)
	// Project settings: Vertex.
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"), `{
		"env": {"CLAUDE_CODE_USE_VERTEX": "1"}
	}`)

	pane := OpenPaneSnapshot{PaneID: "c3", Agent: "claude", Cwd: &proj}
	if back := PaneBackendID("claude", pane); back != "vertex" {
		t.Fatalf("project settings must win: got %q", back)
	}
	if mode := PaneBillingMode("claude", pane, DefaultBillingDeps()); mode != BillingPayAsYouGo {
		t.Fatalf("mode: got %v", mode)
	}
}

func TestIO_ClaudeProcessEnvWinsOverEmptySettings(t *testing.T) {
	_, jsonPath := withTempClaudeConfig(t)
	writeFile(t, jsonPath, `{"oauthAccount":{"billingType":"stripe_subscription"},"cachedUsageUtilization":{"utilization":{}}}`)
	// No settings env — process flag only.
	t.Setenv("CLAUDE_CODE_USE_FOUNDRY", "1")

	cwd := ioTestCwd
	pane := OpenPaneSnapshot{PaneID: "c4", Agent: "claude", Cwd: &cwd}
	if back := PaneBackendID("claude", pane); back != "foundry" {
		t.Fatalf("process env: got %q want foundry", back)
	}
}

func TestIO_BillingProviderFilter_MixedGrokPanes(t *testing.T) {
	// One xAI pane + one ollama pane → grok subscription block stays visible.
	home := withTempGrokHome(t)
	clearClaudeDeployEnv(t)
	writeFile(t, filepath.Join(home, "config.toml"), `
[model.smoke-ollama]
base_url = "http://localhost:11434/v1"
`)
	writeFile(t, filepath.Join(home, "auth.json"), `{
		"https://auth.x.ai::test": {"auth_mode": "oidc"}
	}`)
	writeGrokSession(t, home, "sid-xai", "grok-4.5")
	writeGrokSession(t, home, "sid-ollama", "smoke-ollama")

	cwd := ioTestCwd
	xaiID, ollamaID := "sid-xai", "sid-ollama"
	panes := []OpenPaneSnapshot{
		{PaneID: "gx", Agent: "grok", SessionID: &xaiID, Cwd: &cwd},
		{PaneID: "go", Agent: "grok", SessionID: &ollamaID, Cwd: &cwd},
	}
	set := BillingProviderFilter(panes, true, DefaultBillingDeps())
	if !set["grok"] {
		t.Fatalf("mixed panes must keep grok subscription: %#v", set)
	}
	if PaneBackendID("grok", panes[0]) != "" {
		t.Fatalf("xai pane backend should be empty")
	}
	if PaneBackendID("grok", panes[1]) != "ollama" {
		t.Fatalf("ollama pane backend: got %q", PaneBackendID("grok", panes[1]))
	}
}
