/**
 * Tests for Grok auth / billing parsing.
 */
package limits

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseGrokAuthJSON_PrefersAuthXAI(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"https://accounts.x.ai/sign-in": map[string]any{
			"email": "legacy@example.com", "auth_mode": "session",
		},
		"https://auth.x.ai::abc": map[string]any{
			"email": "super@example.com", "auth_mode": "oidc", "expires_at": "2026-08-01T00:00:00Z",
		},
	})
	got := ParseGrokAuthJSON(string(raw))
	if got == nil || got.Email == nil || *got.Email != "super@example.com" {
		t.Fatalf("%+v", got)
	}
	if got.AuthMode == nil || *got.AuthMode != "oidc" {
		t.Fatalf("%+v", got)
	}
	if ParseGrokAuthJSON("not-json") != nil {
		t.Fatal("expected nil")
	}
}

func TestProviderLimitsFromBillingResult(t *testing.T) {
	email := "a@b.c"
	result := ProviderLimitsFromBillingResult(map[string]any{
		"billingCycle": map[string]any{"billingPeriodEnd": "2026-08-01T00:00:00Z"},
		"monthlyLimit": map[string]any{"val": 10000},
		"usage":        map[string]any{"totalUsed": map[string]any{"val": 2500}},
	}, 1_700_000_000_000, &email)
	if result == nil || result.Primary == nil || result.Primary.UsedPercentage != 25 {
		t.Fatalf("%+v", result)
	}
	if result.PlanType != nil {
		t.Fatal("plan should be unset")
	}
	if result.Note == nil || !containsStr(*result.Note, "$25.00") {
		t.Fatalf("note=%v", result.Note)
	}
	want := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).Unix()
	if result.Primary.ResetsAt == nil || *result.Primary.ResetsAt != want {
		t.Fatalf("resetsAt=%v", result.Primary.ResetsAt)
	}
	if ProviderLimitsFromBillingResult(map[string]any{}, 0, nil) != nil {
		t.Fatal("expected nil")
	}
}
