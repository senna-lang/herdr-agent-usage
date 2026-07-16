/**
 * Tests for ExtractLatestUsageFromLines.
 */
package claude

import (
	"encoding/json"
	"testing"
)

func assistantLine(isSidechain bool, model string, usage map[string]int) string {
	if model == "" {
		model = "claude-sonnet-5"
	}
	if usage == nil {
		usage = map[string]int{
			"input_tokens": 10, "cache_read_input_tokens": 20,
			"cache_creation_input_tokens": 0, "output_tokens": 5,
		}
	}
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "isSidechain": isSidechain,
		"message": map[string]any{"model": model, "usage": usage},
	})
	return string(b)
}

func TestExtractLatestUsageFromLines_LastAssistant(t *testing.T) {
	lines := []string{
		assistantLine(false, "", map[string]int{"input_tokens": 1, "cache_read_input_tokens": 2, "output_tokens": 3}),
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.InputTokens != 1 || got.CacheReadInputTokens != 2 || got.OutputTokens != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsageFromLines_SkipsTrailingMeta(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{"type": "ai-title", "aiTitle": "some title"})
	lines := []string{
		assistantLine(false, "", map[string]int{"input_tokens": 100, "cache_read_input_tokens": 0, "output_tokens": 1}),
		string(meta),
		string(meta),
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.InputTokens != 100 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsageFromLines_SkipsZeroTotal(t *testing.T) {
	lines := []string{
		assistantLine(false, "", map[string]int{"input_tokens": 50, "cache_read_input_tokens": 0, "output_tokens": 1}),
		assistantLine(false, "", map[string]int{"input_tokens": 0, "cache_read_input_tokens": 0, "output_tokens": 0}),
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.InputTokens != 50 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsageFromLines_SkipsSidechain(t *testing.T) {
	lines := []string{
		assistantLine(false, "", map[string]int{"input_tokens": 50, "cache_read_input_tokens": 0, "output_tokens": 1}),
		assistantLine(true, "", map[string]int{"input_tokens": 999, "cache_read_input_tokens": 0, "output_tokens": 1}),
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.InputTokens != 50 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsageFromLines_NoValid(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{"type": "ai-title"})
	if got := ExtractLatestUsageFromLines([]string{string(meta), string(meta)}); got != nil {
		t.Fatalf("got %+v", got)
	}
}
