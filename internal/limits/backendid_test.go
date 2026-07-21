/**
 * Tests for BackendIDFromBaseURL.
 */
package limits

import "testing"

func TestBackendIDFromBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                                      "",
		"https://api.x.ai/v1":                   "xai",
		"https://cli-chat-proxy.grok.com/v1":    "xai",
		"https://api.openai.com/v1":             "openai",
		"https://api.anthropic.com/v1":          "anthropic",
		"http://localhost:11434/v1":             "ollama",
		"http://127.0.0.1:8080/v1":              "local",
		"https://api.together.xyz/v1":           "together",
		"https://api.deepseek.com":              "deepseek",
		"https://openrouter.ai/api/v1":          "openrouter",
		"https://bedrock-runtime.us-east-1.amazonaws.com": "bedrock",
		"https://us-east5-aiplatform.googleapis.com":      "vertex",
		"https://my-gateway.example.com/v1":     "my-gateway",
		"api.openai.com/v1":                     "openai",
	}
	for in, want := range cases {
		if got := BackendIDFromBaseURL(in); got != want {
			t.Fatalf("BackendIDFromBaseURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIsXAIBaseURL(t *testing.T) {
	if !IsXAIBaseURL("https://api.x.ai/v1") {
		t.Fatal("xai should match")
	}
	if IsXAIBaseURL("http://localhost:11434/v1") {
		t.Fatal("ollama must not match xai")
	}
}
