/**
 * Non-Claude primary-limit notify persistence (TS-compatible lock + path).
 */
package limits

import (
	"github.com/senna-lang/herdr-agent-usage/internal/ratelimit"
)

// NotifyProviderPrimaryLimits checks non-Claude primary windows under the
// shared lock, excluding every configured Claude profile id.
func NotifyProviderPrimaryLimits(providers []ProviderLimits, nowMs int64) {
	claudeProfiles := ResolvedClaudeProfiles()
	ratelimit.WithLockedProviderState(func(current ratelimit.ProviderNotifyStateMap) ratelimit.ProviderNotifyStateMap {
		// convert to limits.ProviderNotifyState
		cur := ProviderNotifyState{}
		for k, v := range current {
			cur[k] = v
		}
		next := CheckProviderPrimaryLimits(providers, cur, nowMs, herdrcliShowNotification, claudeProfiles)
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
