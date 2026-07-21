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
	notify NotifyFunc,
) *ratelimit.WindowState {
	primary := provider.Primary
	if primary == nil || primary.ResetsAt == nil {
		return previous
	}

	decision := ratelimit.DecideBucket(
		ratelimit.WindowInput{UsedPercentage: primary.UsedPercentage, ResetsAt: *primary.ResetsAt},
		previous,
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

// CheckProviderPrimaryLimits is the pure state transition for non-Claude
// primary windows. Every configured Claude profile id is excluded (not just
// the literal "claude") because Claude's statusLine already owns its own
// per-profile alerts — without this, a non-default profile like
// "claude-secondary" would double-notify through this generic loop too.
func CheckProviderPrimaryLimits(
	providers []ProviderLimits,
	current ProviderNotifyState,
	nowMs int64,
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
		next[provider.ProviderID] = processProvider(provider, prev, nowMs, notify)
	}
	return next
}
