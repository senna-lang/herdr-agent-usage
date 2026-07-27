/**
 * Delivers threshold alerts for the shortest available rate-limit window of
 * each non-Claude provider. Claude keeps its statusLine-based notifications.
 *
 * Pure state transition used by the event hook and its tests.
 */
package limits

import (
	"github.com/senna-lang/herdr-agent-usage/internal/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/ratelimit"
)

// NotifyFunc shows a toast; returns whether it was displayed.
type NotifyFunc func(title, body string) bool

// ProviderNotifyState is keyed by provider id (codex, opencode, grok).
type ProviderNotifyState map[string]*ratelimit.WindowState

func processProvider(
	provider ProviderLimits,
	previous *ratelimit.WindowState,
	nowMs int64,
	thresholds []int,
	notify NotifyFunc,
) *ratelimit.WindowState {
	primary := provider.Primary
	if primary == nil || primary.ResetsAt == nil {
		return previous
	}

	decision := ratelimit.DecideBucketWithThresholds(
		ratelimit.WindowInput{UsedPercentage: primary.UsedPercentage, ResetsAt: *primary.ResetsAt},
		previous,
		thresholds,
	)
	if decision.BucketToNotify == nil {
		next := decision.NewState
		return &next
	}

	text := ratelimit.FormatProviderPrimaryNotification(
		provider.Label,
		*decision.BucketToNotify,
		*primary.ResetsAt,
		nowMs,
	)
	return ratelimit.ApplyNotifyResult(previous, decision.NewState, notify(text.Title, text.Body))
}

// CheckProviderPrimaryLimitsWithThresholds is the pure state transition for
// non-Claude primary windows. Every configured Claude profile id is excluded
// because Claude's statusLine owns its own per-profile alerts.
func CheckProviderPrimaryLimitsWithThresholds(
	providers []ProviderLimits,
	current ProviderNotifyState,
	nowMs int64,
	thresholds []int,
	notify NotifyFunc,
	claudeProfiles []claude.ClaudeProfile,
) ProviderNotifyState {
	next := make(ProviderNotifyState, len(current)+len(providers))
	for k, v := range current {
		next[k] = v
	}
	for _, provider := range providers {
		if claude.IsClaudeProviderID(provider.ProviderID, claudeProfiles) {
			continue
		}
		prev := current[provider.ProviderID]
		next[provider.ProviderID] = processProvider(provider, prev, nowMs, thresholds, notify)
	}
	return next
}

// CheckProviderPrimaryLimits uses the default 50/20/10/5% thresholds.
func CheckProviderPrimaryLimits(
	providers []ProviderLimits,
	current ProviderNotifyState,
	nowMs int64,
	notify NotifyFunc,
	claudeProfiles []claude.ClaudeProfile,
) ProviderNotifyState {
	return CheckProviderPrimaryLimitsWithThresholds(providers, current, nowMs, nil, notify, claudeProfiles)
}
