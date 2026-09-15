/**
 * Antigravity CLI limit collection.
 *
 * Antigravity keeps no local limits file the way Claude's statusLine cache
 * does; its statusLine payload is the only local surface, exactly like this
 * provider's context resolution. The collector therefore reads the newest
 * fresh snapshot across every session under sessionsDir rather than one keyed
 * by pane or session: quota is billed per Google account, not per
 * conversation, so whichever pane last reported the account's usage speaks
 * for the whole account.
 */
package limits

import (
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/antigravity"
)

// CollectAntigravityLimitsOptions injects the sessions directory for tests.
type CollectAntigravityLimitsOptions struct {
	SessionsDir string
}

// antigravityWeeklyWindowMinutes is the duration Antigravity itself reports
// for both quota buckets (a rolling week); recorded here only as a display
// hint, since ResetsAt (parsed from the payload's own reset_time) is what
// actually drives the countdown.
const antigravityWeeklyWindowMinutes = 10080

// CollectAntigravityLimits builds a ProviderLimits from the freshest
// Antigravity statusLine snapshot across every known session.
//
// Antigravity tracks two separate weekly allotments rather than one quota on
// multiple time windows: "gemini-weekly" for its own model family and
// "3p-weekly" for third-party models (Claude, GPT, ...) routed through it.
// gemini-weekly is shown as Primary since Antigravity's own model is the
// account's native quota; 3p-weekly as Secondary. A pane actively spending
// third-party quota still sees both numbers, just not as the headline one.
func CollectAntigravityLimits(nowMs int64, opts CollectAntigravityLimitsOptions) ProviderLimits {
	const providerID = "agy"
	const label = "Antigravity"

	sessionsDir := opts.SessionsDir
	if sessionsDir == "" {
		stateDir := antigravity.StateDir()
		if stateDir == "" {
			note := "no home directory"
			return ProviderLimits{ProviderID: providerID, Label: label, Source: "none", FetchedAtMs: nowMs, Note: &note}
		}
		sessionsDir = antigravity.SessionsDir(stateDir)
	}

	snap, ok := antigravity.LatestFreshSnapshot(sessionsDir, nowMs)
	if !ok {
		note := "no statusLine observation yet — see docs/antigravity-contract.md"
		return ProviderLimits{ProviderID: providerID, Label: label, Source: "none", FetchedAtMs: nowMs, Note: &note}
	}

	pl := ProviderLimits{
		ProviderID:  providerID,
		Label:       label,
		Source:      "antigravity statusLine",
		FetchedAtMs: nowMs,
	}
	if snap.PlanTier != "" {
		plan := snap.PlanTier
		pl.PlanType = &plan
	}
	if w, ok := snap.Quota["gemini-weekly"]; ok {
		pl.Primary = quotaLimitWindow(w)
	}
	if w, ok := snap.Quota["3p-weekly"]; ok {
		pl.Secondary = quotaLimitWindow(w)
	}
	return pl
}

// quotaLimitWindow converts one statusLine quota bucket into a LimitWindow.
// ResetsAt is parsed from the bucket's own absolute reset_time rather than
// derived from a relative "seconds remaining" figure, which would only be
// accurate at the instant it was captured.
func quotaLimitWindow(w antigravity.QuotaWindow) *LimitWindow {
	used := (1 - w.RemainingFraction) * 100
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	window := LimitWindow{UsedPercentage: used}
	minutes := antigravityWeeklyWindowMinutes
	window.WindowMinutes = &minutes
	if t, err := time.Parse(time.RFC3339, w.ResetTime); err == nil {
		resetsAt := t.Unix()
		window.ResetsAt = &resetsAt
	}
	return &window
}
