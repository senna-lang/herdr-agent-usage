/**
 * Tests for ParseRateLimits.
 */
package ratelimit

import (
	"encoding/json"
	"testing"
)

func TestParseRateLimits_Both(t *testing.T) {
	jsonStr := `{"rate_limits":{"five_hour":{"used_percentage":72,"resets_at":100},"seven_day":{"used_percentage":43,"resets_at":200}}}`
	result := ParseRateLimits(jsonStr)
	if result == nil || result.FiveHour == nil || result.FiveHour.UsedPercentage != 72 || result.FiveHour.ResetsAt != 100 {
		t.Fatalf("fiveHour=%+v", result)
	}
	if result.SevenDay == nil || result.SevenDay.UsedPercentage != 43 {
		t.Fatalf("sevenDay=%+v", result)
	}
}

func TestParseRateLimits_Missing(t *testing.T) {
	result := ParseRateLimits(`{"session_id":"abc"}`)
	if result == nil || result.FiveHour != nil || result.SevenDay != nil {
		t.Fatalf("%+v", result)
	}
}

func TestParseRateLimits_OneWindow(t *testing.T) {
	result := ParseRateLimits(`{"rate_limits":{"five_hour":{"used_percentage":10,"resets_at":100}}}`)
	if result == nil || result.FiveHour == nil || result.FiveHour.UsedPercentage != 10 || result.SevenDay != nil {
		t.Fatalf("%+v", result)
	}
}

func TestParseRateLimits_ZeroUsed(t *testing.T) {
	result := ParseRateLimits(`{"rate_limits":{"five_hour":{"used_percentage":0,"resets_at":100}}}`)
	if result == nil || result.FiveHour == nil || result.FiveHour.UsedPercentage != 0 {
		t.Fatalf("%+v", result)
	}
}

func TestParseRateLimits_NoResetsAt(t *testing.T) {
	result := ParseRateLimits(`{"rate_limits":{"five_hour":{"used_percentage":50}}}`)
	if result == nil || result.FiveHour != nil {
		t.Fatalf("%+v", result)
	}
}

func TestParseRateLimits_Invalid(t *testing.T) {
	if ParseRateLimits("{not valid json") != nil {
		t.Fatal("expected nil")
	}
	// sanity: valid empty object
	_ = json.Valid([]byte(`{}`))
}
