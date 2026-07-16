/**
 * Tests for Codex rate_limits extraction.
 */
package limits

import (
	"encoding/json"
	"testing"
)

func tokenCountRateLine(primary, secondary map[string]any, plan string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{
				"last_token_usage":     map[string]any{"total_tokens": 100},
				"model_context_window": 200_000,
			},
			"rate_limits": map[string]any{
				"primary": primary, "secondary": secondary, "plan_type": plan,
			},
		},
	})
	return string(b)
}

func TestExtractRateLimitsFromLines(t *testing.T) {
	lines := []string{tokenCountRateLine(
		map[string]any{"used_percent": 3, "window_minutes": 300, "resets_at": 100},
		map[string]any{"used_percent": 10, "window_minutes": 10080, "resets_at": 200},
		"plus",
	)}
	got := ExtractRateLimitsFromLines(lines)
	if got == nil || got.Primary == nil || got.Primary.UsedPercentage != 3 {
		t.Fatalf("%+v", got)
	}
	if got.Primary.ResetsAt == nil || *got.Primary.ResetsAt != 100 {
		t.Fatalf("primary resets %+v", got.Primary)
	}
	if got.Secondary == nil || got.Secondary.UsedPercentage != 10 {
		t.Fatalf("secondary %+v", got.Secondary)
	}
	if got.PlanType == nil || *got.PlanType != "plus" {
		t.Fatalf("plan %+v", got.PlanType)
	}
}

func TestExtractRateLimitsFromLines_Latest(t *testing.T) {
	lines := []string{
		tokenCountRateLine(map[string]any{"used_percent": 1, "window_minutes": 300, "resets_at": 1}, nil, "plus"),
		tokenCountRateLine(
			map[string]any{"used_percent": 50, "window_minutes": 300, "resets_at": 2},
			map[string]any{"used_percent": 20, "window_minutes": 10080, "resets_at": 3},
			"plus",
		),
	}
	got := ExtractRateLimitsFromLines(lines)
	if got == nil || got.Primary == nil || got.Primary.UsedPercentage != 50 {
		t.Fatalf("%+v", got)
	}
	if got.Secondary == nil || got.Secondary.UsedPercentage != 20 {
		t.Fatalf("%+v", got.Secondary)
	}
}

func TestExtractRateLimitsFromLines_Missing(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{"last_token_usage": map[string]any{"total_tokens": 1}},
		},
	})
	if ExtractRateLimitsFromLines([]string{string(b)}) != nil {
		t.Fatal("expected nil")
	}
}
