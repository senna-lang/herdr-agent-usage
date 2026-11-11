/**
 * Delivers threshold alerts for the shortest available rate-limit window of
 * each non-Claude provider. Claude keeps its statusLine-based notifications.
 *
 * Pure state transition used by the event hook and its tests.
 */
package limits

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/ratelimit"
)

// NotifyFunc shows a toast; returns whether it was displayed.
type NotifyFunc func(title, body string) bool

// ProviderNotifyState is keyed by provider id (codex, opencode, grok).
type ProviderNotifyState map[string]*ratelimit.WindowState

func processProviderWith(
	provider ProviderLimits,
	previous *ratelimit.WindowState,
	nowMs int64,
	thresholds []int,
	deliver ratelimit.DeliverFn,
	percent core.LimitPercent,
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

	heading, account := toastHeading(provider)
	text := ratelimit.FormatProviderPrimaryNotification(
		heading,
		*decision.BucketToNotify,
		*primary.ResetsAt,
		nowMs,
		percent,
	)
	text.Title = ratelimit.WithAccountLabel(text.Title, account)
	return ratelimit.ApplyNotifyResult(previous, decision.NewState, deliver(*decision.BucketToNotify, text))
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
	percent core.LimitPercent,
) ProviderNotifyState {
	return checkProviderPrimaryLimitsWith(providers, current, nowMs, thresholds, func(_ ProviderLimits) ratelimit.DeliverFn {
		return func(_ ratelimit.Bucket, text ratelimit.NotificationText) bool { return notify(text.Title, text.Body) }
	}, claudeProfiles, percent)
}

// checkProviderPrimaryLimitsWith is the state transition with a per-provider
// delivery, so a caller can see which provider and bucket fired.
func checkProviderPrimaryLimitsWith(
	providers []ProviderLimits,
	current ProviderNotifyState,
	nowMs int64,
	thresholds []int,
	deliverFor func(ProviderLimits) ratelimit.DeliverFn,
	claudeProfiles []claude.ClaudeProfile,
	percent core.LimitPercent,
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
		next[provider.ProviderID] = processProviderWith(provider, prev, nowMs, thresholds, deliverFor(provider), percent)
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
	return CheckProviderPrimaryLimitsWithThresholds(providers, current, nowMs, nil, notify, claudeProfiles, core.LimitPercentRemaining)
}

// toastHeading picks the provider name and account a toast title shows. An
// entry grouped with other accounts of its family is titled by the family
// ("Codex limit") and names its own account; a lone entry keeps its label
// and names no account, as before.
func toastHeading(provider ProviderLimits) (heading, account string) {
	if provider.GroupLabel == "" {
		return provider.Label, ""
	}
	account = provider.ProfileLabel
	if account == "" {
		account = provider.Label
	}
	return provider.GroupLabel, account
}
