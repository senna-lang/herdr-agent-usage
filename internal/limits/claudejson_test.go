/**
 * Tests for parsing Claude's cachedUsageUtilization.
 */
package limits

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWindowFromUtilization(t *testing.T) {
	u := 63.0
	iso := "2026-07-19T20:00:00.000Z"
	got := WindowFromUtilization(&u, &iso, 10080)
	if got == nil || got.UsedPercentage != 63 || got.WindowMinutes == nil || *got.WindowMinutes != 10080 {
		t.Fatalf("got %+v", got)
	}
	wantSec := time.Date(2026, 7, 19, 20, 0, 0, 0, time.UTC).Unix()
	if got.ResetsAt == nil || *got.ResetsAt != wantSec {
		t.Fatalf("resetsAt=%v want %d", got.ResetsAt, wantSec)
	}
	if WindowFromUtilization(nil, nil, 300) != nil {
		t.Fatal("expected nil")
	}
}

func TestProviderLimitsFromClaudeJSON(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"oauthAccount": map[string]any{
			"billingType": "stripe_subscription", "organizationType": "claude_pro",
		},
		"cachedUsageUtilization": map[string]any{
			"fetchedAtMs": 1_700_000_000_000,
			"utilization": map[string]any{
				"five_hour": map[string]any{"utilization": 10, "resets_at": "2026-07-15T16:00:00.000Z"},
				"seven_day": map[string]any{"utilization": 50, "resets_at": "2026-07-20T00:00:00.000Z"},
			},
		},
	})
	result := ProviderLimitsFromClaudeJSON(string(raw), 1_700_000_000_000)
	if result == nil || result.Primary == nil || result.Primary.UsedPercentage != 10 {
		t.Fatalf("primary %+v", result)
	}
	if result.Secondary == nil || result.Secondary.UsedPercentage != 50 {
		t.Fatalf("secondary %+v", result)
	}
	if result.PlanType == nil || *result.PlanType != "Pro" {
		t.Fatalf("plan %+v", result.PlanType)
	}
	if result.Source == "" || !containsStr(result.Source, "cachedUsageUtilization") {
		t.Fatalf("source %q", result.Source)
	}
	if ProviderLimitsFromClaudeJSON("{}", 0) != nil {
		t.Fatal("expected nil")
	}
}

func TestProviderLimitsFromClaudeJSON_TeamOrg(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"oauthAccount": map[string]any{
			"organizationType": "claude_team",
		},
		"cachedUsageUtilization": map[string]any{
			"fetchedAtMs": 1_700_000_000_000,
			"utilization": map[string]any{
				"five_hour": map[string]any{"utilization": 10, "resets_at": "2026-07-15T16:00:00.000Z"},
			},
		},
	})
	result := ProviderLimitsFromClaudeJSON(string(raw), 1_700_000_000_000)
	if result == nil || result.PlanType == nil || *result.PlanType != "Team" {
		t.Fatalf("plan %+v", result)
	}
}

func TestProviderLimitsFromClaudeJSON_RateLimitTierSingularKey(t *testing.T) {
	// The real ~/.claude.json key is organizationRateLimitTier (singular).
	// organizationType is absent here so the label must come from the tier.
	raw, _ := json.Marshal(map[string]any{
		"oauthAccount": map[string]any{
			"organizationRateLimitTier": "team_tier",
		},
		"cachedUsageUtilization": map[string]any{
			"fetchedAtMs": 1_700_000_000_000,
			"utilization": map[string]any{
				"five_hour": map[string]any{"utilization": 10, "resets_at": "2026-07-15T16:00:00.000Z"},
			},
		},
	})
	result := ProviderLimitsFromClaudeJSON(string(raw), 1_700_000_000_000)
	if result == nil || result.PlanType == nil || *result.PlanType != "Team" {
		t.Fatalf("plan %+v", result)
	}
}

func TestProviderLimitsFromClaudeJSON_UserRateLimitTierFallback(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"oauthAccount": map[string]any{
			"userRateLimitTier": "max_5x",
		},
		"cachedUsageUtilization": map[string]any{
			"fetchedAtMs": 1_700_000_000_000,
			"utilization": map[string]any{
				"five_hour": map[string]any{"utilization": 10, "resets_at": "2026-07-15T16:00:00.000Z"},
			},
		},
	})
	result := ProviderLimitsFromClaudeJSON(string(raw), 1_700_000_000_000)
	if result == nil || result.PlanType == nil || *result.PlanType != "Max 5x" {
		t.Fatalf("plan %+v", result)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
