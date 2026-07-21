/**
 * Tests for Claude deployment env / settings backend detection.
 */
package limits

import "testing"

func TestClaudeBackendFromEnv_CloudFlags(t *testing.T) {
	if got := ClaudeBackendFromEnv(map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1"}); got != "bedrock" {
		t.Fatalf("bedrock: got %q", got)
	}
	if got := ClaudeBackendFromEnv(map[string]string{"CLAUDE_CODE_USE_VERTEX": "true"}); got != "vertex" {
		t.Fatalf("vertex: got %q", got)
	}
	if got := ClaudeBackendFromEnv(map[string]string{"CLAUDE_CODE_USE_FOUNDRY": "yes"}); got != "foundry" {
		t.Fatalf("foundry: got %q", got)
	}
	if got := ClaudeBackendFromEnv(map[string]string{"CLAUDE_CODE_USE_MANTLE": "1"}); got != "bedrock" {
		t.Fatalf("mantle: got %q", got)
	}
}

func TestClaudeBackendFromEnv_BaseURL(t *testing.T) {
	if got := ClaudeBackendFromEnv(map[string]string{
		"ANTHROPIC_BASE_URL": "https://api.portkey.ai",
	}); got != "portkey" {
		t.Fatalf("portkey host: got %q", got)
	}
	if got := ClaudeBackendFromEnv(map[string]string{
		"ANTHROPIC_BEDROCK_BASE_URL": "https://my-llm-gateway.com/bedrock",
	}); got != "my-llm-gateway" {
		t.Fatalf("bedrock base host: got %q", got)
	}
	// Flag wins over URL.
	if got := ClaudeBackendFromEnv(map[string]string{
		"CLAUDE_CODE_USE_BEDROCK": "1",
		"ANTHROPIC_BASE_URL":      "https://api.portkey.ai",
	}); got != "bedrock" {
		t.Fatalf("flag must win: got %q", got)
	}
	if got := ClaudeBackendFromEnv(nil); got != "" {
		t.Fatalf("nil: got %q", got)
	}
	if got := ClaudeBackendFromEnv(map[string]string{}); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}

func TestClaudeBackendFromEnv_AnthropicBaseURLGateway(t *testing.T) {
	// Host that BackendID maps back to "anthropic" still means a gateway
	// override of the default API — label as gateway.
	if got := ClaudeBackendFromEnv(map[string]string{
		"ANTHROPIC_BASE_URL": "https://api.anthropic.com",
	}); got != "gateway" {
		t.Fatalf("anthropic host via ANTHROPIC_BASE_URL should be gateway: got %q", got)
	}
}

func TestClaudeBillingModeFromEnv(t *testing.T) {
	if got := ClaudeBillingModeFromEnv(map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1"}); got != BillingPayAsYouGo {
		t.Fatalf("got %v", got)
	}
	if got := ClaudeBillingModeFromEnv(nil); got != BillingUnknown {
		t.Fatalf("nil: got %v", got)
	}
}

func TestClaudeEnvFromSettingsJSON(t *testing.T) {
	raw := `{"env":{"CLAUDE_CODE_USE_BEDROCK":"1","AWS_REGION":"us-east-1"},"model":"opus"}`
	env := ClaudeEnvFromSettingsJSON(raw)
	if env["CLAUDE_CODE_USE_BEDROCK"] != "1" || env["AWS_REGION"] != "us-east-1" {
		t.Fatalf("got %#v", env)
	}
	if ClaudeEnvFromSettingsJSON(`{}`) != nil {
		t.Fatal("empty env should be nil")
	}
	if ClaudeEnvFromSettingsJSON("nope") != nil {
		t.Fatal("bad json")
	}
}

func TestMergeClaudeEnv(t *testing.T) {
	got := MergeClaudeEnv(
		map[string]string{"A": "1", "B": "2"},
		map[string]string{"B": "9", "C": "3"},
	)
	if got["A"] != "1" || got["B"] != "9" || got["C"] != "3" {
		t.Fatalf("got %#v", got)
	}
}

func TestResolveClaudeBackendID(t *testing.T) {
	if got := ResolveClaudeBackendID(nil); got != "anthropic" {
		t.Fatalf("fallback: got %q", got)
	}
	if got := ResolveClaudeBackendID(map[string]string{"CLAUDE_CODE_USE_VERTEX": "1"}); got != "vertex" {
		t.Fatalf("vertex: got %q", got)
	}
}
