/**
 * Tests for OpenCode message token extraction.
 */
package opencode

import (
	"encoding/json"
	"testing"
)

func TestContextTokensFromMessageTokens(t *testing.T) {
	got := ContextTokensFromMessageTokens(506, 23_710, 0)
	if got == nil || *got != 506+23_710 {
		t.Fatalf("got %#v", got)
	}
	if ContextTokensFromMessageTokens(0, 0, 0) != nil {
		t.Fatal("expected nil for zeros")
	}
}

func TestUsageFromMessageData(t *testing.T) {
	got := UsageFromMessageData(map[string]any{
		"role": "assistant", "modelID": "minimax-m3", "providerID": "opencode-go",
		"tokens": map[string]any{"input": float64(100), "cache": map[string]any{"read": float64(50), "write": float64(10)}},
	})
	if got == nil || got.ContextTokens != 160 || got.ModelID == nil || *got.ModelID != "minimax-m3" {
		t.Fatalf("got %+v", got)
	}

	got = UsageFromMessageData(map[string]any{
		"role":   "assistant",
		"model":  map[string]any{"modelID": "minimax-m3", "providerID": "opencode-go"},
		"tokens": map[string]any{"input": float64(10), "cache": map[string]any{"read": float64(0)}},
	})
	if got == nil || got.ContextTokens != 10 {
		t.Fatalf("nested model got %+v", got)
	}

	if UsageFromMessageData(map[string]any{"role": "user", "tokens": map[string]any{"input": float64(100)}}) != nil {
		t.Fatal("user should be nil")
	}
}

func TestUsageFromLatestMessageJSONs(t *testing.T) {
	rows := []string{
		mustJSON(map[string]any{"role": "user"}),
		mustJSON(map[string]any{
			"role": "assistant", "modelID": "minimax-m3", "providerID": "opencode-go",
			"tokens": map[string]any{"input": 200, "cache": map[string]any{"read": 800}},
		}),
		mustJSON(map[string]any{
			"role": "assistant", "tokens": map[string]any{"input": 1, "cache": map[string]any{"read": 1}},
		}),
	}
	got := UsageFromLatestMessageJSONs(rows)
	if got == nil || got.ContextTokens != 1000 {
		t.Fatalf("got %+v", got)
	}

	if UsageFromLatestMessageJSONs([]string{
		mustJSON(map[string]any{"role": "user"}),
		mustJSON(map[string]any{"role": "assistant"}),
	}) != nil {
		t.Fatal("expected nil")
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
