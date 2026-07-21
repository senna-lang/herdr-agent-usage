/**
 * Tests for Grok custom-model backend detection.
 */
package limits

import "testing"

func TestParseGrokModelConfigs(t *testing.T) {
	body := `
# comment
[models]
default = "grok-4.5"

[model.ollama-codellama]
model = "codellama"
base_url = "http://localhost:11434/v1"  # local
name = "CodeLlama"

[model.claude-opus]
base_url = "https://api.anthropic.com/v1"
model = "claude-opus-4-6"

[other]
base_url = "ignored"
`
	got := ParseGrokModelConfigs(body)
	if len(got) != 2 {
		t.Fatalf("got %d models: %#v", len(got), got)
	}
	if got["ollama-codellama"].BaseURL != "http://localhost:11434/v1" {
		t.Fatalf("ollama base: %#v", got["ollama-codellama"])
	}
	if got["claude-opus"].Model != "claude-opus-4-6" {
		t.Fatalf("claude model field: %#v", got["claude-opus"])
	}
}

func TestParseGrokModelsBaseURL(t *testing.T) {
	body := `
[endpoints]
models_base_url = "https://grok-proxy.acme.com/v1"
`
	if got := ParseGrokModelsBaseURL(body); got != "https://grok-proxy.acme.com/v1" {
		t.Fatalf("got %q", got)
	}
}

func TestGrokBackendIDForModel(t *testing.T) {
	models := map[string]GrokModelConfig{
		"ollama-codellama": {BaseURL: "http://localhost:11434/v1", Model: "codellama"},
		"gpt-4o":           {BaseURL: "https://api.openai.com/v1"},
		"grok-build":       {BaseURL: ""}, // override without URL
	}
	if got := GrokBackendIDForModel("ollama-codellama", models, ""); got != "ollama" {
		t.Fatalf("ollama section: got %q", got)
	}
	// Match by model= field.
	if got := GrokBackendIDForModel("codellama", models, ""); got != "ollama" {
		t.Fatalf("model field match: got %q", got)
	}
	if got := GrokBackendIDForModel("gpt-4o", models, ""); got != "openai" {
		t.Fatalf("openai: got %q", got)
	}
	if got := GrokBackendIDForModel("grok-build", models, ""); got != "xai" {
		t.Fatalf("built-in override: got %q", got)
	}
	if got := GrokBackendIDForModel("grok-4.5", models, ""); got != "xai" {
		t.Fatalf("catalog model: got %q", got)
	}
	if got := GrokBackendIDForModel("anything", nil, "https://api.together.xyz/v1"); got != "together" {
		t.Fatalf("global base: got %q", got)
	}
	if got := GrokBackendIDForModel("", nil, ""); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}

func TestGrokBillingModeFromBackendID(t *testing.T) {
	if got := GrokBillingModeFromBackendID("ollama"); got != BillingPayAsYouGo {
		t.Fatalf("ollama: got %v", got)
	}
	if got := GrokBillingModeFromBackendID("xai"); got != BillingUnknown {
		t.Fatalf("xai: got %v", got)
	}
	if got := GrokBillingModeFromBackendID(""); got != BillingUnknown {
		t.Fatalf("empty: got %v", got)
	}
}

func TestGrokModelIDFromLines(t *testing.T) {
	lines := []string{
		`{"params":{"update":{"sessionUpdate":"turn_started","modelId":"old"}}}`,
		`{"params":{"update":{"sessionUpdate":"turn_completed","modelId":"grok-4.5","usage":{"totalTokens":1}}}}`,
		`{"params":{"update":{"sessionUpdate":"tool_call","_meta":{"modelId":"should-not-win"}}}}`,
	}
	// Reverse scan: last modelId wins — tool_call has only _meta.modelId
	if got := GrokModelIDFromLines(lines); got != "should-not-win" {
		t.Fatalf("got %q want should-not-win (most recent)", got)
	}
	// Prefer top-level modelId on turn_completed when it's last.
	lines2 := []string{
		`{"params":{"update":{"sessionUpdate":"turn_completed","modelId":"gpt-4o"}}}`,
	}
	if got := GrokModelIDFromLines(lines2); got != "gpt-4o" {
		t.Fatalf("got %q", got)
	}
}

func TestGrokModelIDFromSummaryJSON(t *testing.T) {
	if got := GrokModelIDFromSummaryJSON(`{"current_model_id":"ollama-codellama"}`); got != "ollama-codellama" {
		t.Fatalf("got %q", got)
	}
	if got := GrokModelIDFromSummaryJSON("nope"); got != "" {
		t.Fatalf("bad json: got %q", got)
	}
}

func TestResolveGrokBackendID(t *testing.T) {
	if got := ResolveGrokBackendID("", nil, ""); got != "xai" {
		t.Fatalf("fallback: got %q", got)
	}
}
