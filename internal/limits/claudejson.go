/**
 * Reads rate-limit windows from cachedUsageUtilization in ~/.claude.json.
 */
package limits

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/planlabels"
)

type utilizationWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

// ResolveClaudeJSONPath returns CLAUDE_CONFIG_JSON or ~/.claude.json.
func ResolveClaudeJSONPath() string {
	if v := os.Getenv("CLAUDE_CONFIG_JSON"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

func parseResetsAtEpochSeconds(iso string) *int64 {
	if iso == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		t, err = time.Parse(time.RFC3339, iso)
	}
	if err != nil {
		return nil
	}
	sec := t.Unix()
	return &sec
}

// WindowFromUtilization maps utilization (used %) to LimitWindow.
func WindowFromUtilization(utilization *float64, resetsAtISO *string, windowMinutes int) *LimitWindow {
	if utilization == nil || math.IsNaN(*utilization) || math.IsInf(*utilization, 0) {
		return nil
	}
	w := &LimitWindow{UsedPercentage: *utilization, WindowMinutes: &windowMinutes}
	if resetsAtISO != nil {
		if sec := parseResetsAtEpochSeconds(*resetsAtISO); sec != nil {
			w.ResetsAt = sec
		}
	}
	return w
}

// ProviderLimitsFromClaudeJSON builds ProviderLimits from a ~/.claude.json body.
func ProviderLimitsFromClaudeJSON(rawJSON string, nowMs int64) *ProviderLimits {
	var parsed struct {
		CachedUsageUtilization *struct {
			FetchedAtMs *float64 `json:"fetchedAtMs"`
			Utilization *struct {
				FiveHour *utilizationWindow `json:"five_hour"`
				SevenDay *utilizationWindow `json:"seven_day"`
			} `json:"utilization"`
		} `json:"cachedUsageUtilization"`
		OAuthAccount *struct {
			OrganizationType           *string `json:"organizationType"`
			OrganizationRateLimitTiers *string `json:"organizationRateLimitTiers"`
			SeatTier                   *string `json:"seatTier"`
		} `json:"oauthAccount"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &parsed); err != nil {
		return nil
	}
	cache := parsed.CachedUsageUtilization
	if cache == nil || cache.Utilization == nil {
		return nil
	}
	var primary, secondary *LimitWindow
	if cache.Utilization.FiveHour != nil {
		primary = WindowFromUtilization(cache.Utilization.FiveHour.Utilization, cache.Utilization.FiveHour.ResetsAt, 300)
	}
	if cache.Utilization.SevenDay != nil {
		secondary = WindowFromUtilization(cache.Utilization.SevenDay.Utilization, cache.Utilization.SevenDay.ResetsAt, 10080)
	}
	if primary == nil && secondary == nil {
		return nil
	}

	var org, tier *string
	if parsed.OAuthAccount != nil {
		if parsed.OAuthAccount.OrganizationType != nil {
			org = parsed.OAuthAccount.OrganizationType
		} else {
			org = parsed.OAuthAccount.SeatTier
		}
		tier = parsed.OAuthAccount.OrganizationRateLimitTiers
	}
	plan := planlabels.ClaudePlanLabel(org, tier)

	fetchedAtMs := nowMs
	if cache.FetchedAtMs != nil && !math.IsNaN(*cache.FetchedAtMs) {
		fetchedAtMs = int64(*cache.FetchedAtMs)
	}
	ageMin := int(math.Max(0, math.Round(float64(nowMs-fetchedAtMs)/60_000)))
	out := ProviderLimits{
		ProviderID:  "claude",
		Label:       "Claude",
		Primary:     primary,
		Secondary:   secondary,
		PlanType:    plan,
		Source:      "claude.json cachedUsageUtilization",
		FetchedAtMs: fetchedAtMs,
	}
	if ageMin > 120 {
		note := "stale ~" + itoa(ageMin) + "m ago"
		out.Note = &note
	}
	return &out
}

// CollectClaudeLimitsFromJSON reads path and parses limits.
func CollectClaudeLimitsFromJSON(nowMs int64, path string) *ProviderLimits {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return ProviderLimitsFromClaudeJSON(string(raw), nowMs)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
