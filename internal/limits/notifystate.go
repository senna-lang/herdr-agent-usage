/**
 * Non-Claude primary-limit notify persistence (TS-compatible lock + path).
 */
package limits

import (
	"github.com/senna-lang/herdr-agent-usage/internal/ratelimit"
)

// NotifyProviderPrimaryLimits checks non-Claude primary windows with default
// thresholds under the shared notification-state lock.
func NotifyProviderPrimaryLimits(providers []ProviderLimits, nowMs int64) {
	NotifyProviderPrimaryLimitsWithThresholds(providers, nowMs, nil)
}

// NotifyProviderPrimaryLimitsWithThresholds applies plugin-configured
// remaining-percent thresholds under the shared notification-state lock.
func NotifyProviderPrimaryLimitsWithThresholds(providers []ProviderLimits, nowMs int64, thresholds []int) {
	claudeProfiles := ResolvedClaudeProfiles()
	ratelimit.WithLockedProviderState(func(current ratelimit.ProviderNotifyStateMap) ratelimit.ProviderNotifyStateMap {
		cur := ProviderNotifyState{}
		for k, v := range current {
			cur[k] = v
		}
		next := CheckProviderPrimaryLimitsWithThresholds(providers, cur, nowMs, thresholds, herdrcliShowNotification, claudeProfiles)
		out := ratelimit.ProviderNotifyStateMap{}
		for k, v := range next {
			out[k] = v
		}
		return out
	})
}

var herdrcliShowNotification = func(title, body string) bool {
	return false
}

// SetShowNotification injects the toast backend (usually herdrcli.ShowNotification).
func SetShowNotification(fn func(title, body string) bool) {
	if fn != nil {
		herdrcliShowNotification = fn
	}
}
