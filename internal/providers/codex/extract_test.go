/**
 * Tests for ExtractLatestUsageFromLines (Codex).
 */
package codex

import (
	"encoding/json"
	"testing"
)

func tokenCountLine(lastTotal, window int, omitLast bool) string {
	var last any
	if !omitLast {
		last = map[string]any{
			"input_tokens": 1000, "cached_input_tokens": 500,
			"output_tokens": 50, "total_tokens": lastTotal,
		}
	}
	b, _ := json.Marshal(map[string]any{
		"timestamp": "2026-07-12T02:12:35.917Z",
		"type":      "event_msg",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{
				"total_token_usage": map[string]any{
					"input_tokens": 999_999, "total_tokens": 1_000_000,
				},
				"last_token_usage":     last,
				"model_context_window": window,
			},
		},
	})
	return string(b)
}

func TestExtractLatestUsage_Basic(t *testing.T) {
	other, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"type": "message"}})
	lines := []string{tokenCountLine(14_523, 353_400, false), string(other)}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.ContextTokens != 14_523 || got.WindowTokens == nil || *got.WindowTokens != 353_400 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsage_MostRecent(t *testing.T) {
	lines := []string{tokenCountLine(1000, 353_400, false), tokenCountLine(50_000, 353_400, false)}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.ContextTokens != 50_000 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsage_NotCumulative(t *testing.T) {
	got := ExtractLatestUsageFromLines([]string{tokenCountLine(14_523, 353_400, false)})
	if got == nil || got.ContextTokens != 14_523 || got.ContextTokens == 1_000_000 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsage_FallbackInput(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{
				"last_token_usage":     map[string]any{"input_tokens": 12_000},
				"model_context_window": 200_000,
			},
		},
	})
	got := ExtractLatestUsageFromLines([]string{string(b)})
	if got == nil || got.ContextTokens != 12_000 || got.WindowTokens == nil || *got.WindowTokens != 200_000 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsage_OmitWindow(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{
				"last_token_usage": map[string]any{"total_tokens": 1000},
			},
		},
	})
	got := ExtractLatestUsageFromLines([]string{string(b)})
	if got == nil || got.ContextTokens != 1000 || got.WindowTokens != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsage_NoTokenCount(t *testing.T) {
	other, _ := json.Marshal(map[string]any{"type": "response_item"})
	if got := ExtractLatestUsageFromLines([]string{string(other), string(other)}); got != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsage_SkipZero(t *testing.T) {
	lines := []string{tokenCountLine(5000, 100_000, false), tokenCountLine(0, 100_000, false)}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.ContextTokens != 5000 {
		t.Fatalf("got %+v", got)
	}
}
