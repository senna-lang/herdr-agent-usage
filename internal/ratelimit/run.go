/**
 * Reads rate_limits from the statusLine stdin and sends notifications when
 * a window crosses a bucket threshold (5h and 7d). Toast body presentation
 * follows LimitPercent; bucket decisions stay remaining-based.
 */
package ratelimit

import "github.com/senna-lang/herdr-agent-usage/internal/core"

// ShowNotificationFn displays a toast; returns whether it was shown.
type ShowNotificationFn func(title, body string) bool

// ProcessWindow applies the configured bucket decision and notification for one Claude window.
func ProcessWindow(
	key WindowKey,
	input *RateWindowInput,
	previous *WindowState,
	nowMs int64,
	thresholds []int,
	notify ShowNotificationFn,
	percent core.LimitPercent,
) *WindowState {
	if input == nil {
		return previous
	}
	decision := DecideBucketWithThresholds(WindowInput{
		UsedPercentage: input.UsedPercentage,
		ResetsAt:       input.ResetsAt,
	}, previous, thresholds)
	if decision.BucketToNotify == nil {
		next := decision.NewState
		return &next
	}
	text := FormatNotificationBody(key, *decision.BucketToNotify, input.ResetsAt, nowMs, percent)
	shown := false
	if notify != nil {
		shown = notify(text.Title, text.Body)
	}
	return ApplyNotifyResult(previous, decision.NewState, shown)
}

// RunRateLimitCheck parses stdin JSON and updates Claude notify state under lock
// in the default (env/CLAUDE_CONFIG_DIR-derived) state dir.
func RunRateLimitCheck(stdinJSON string, nowMs int64, notify ShowNotificationFn) {
	RunRateLimitCheckIn("", stdinJSON, nowMs, notify)
}

// RunRateLimitCheckIn is RunRateLimitCheck scoped to an explicit per-profile
// state dir, so two Claude accounts keep independent notify state.
func RunRateLimitCheckIn(stateDir string, stdinJSON string, nowMs int64, notify ShowNotificationFn) {
	RunRateLimitCheckWithThresholdsIn(stateDir, stdinJSON, nowMs, nil, notify, "")
}

// RunRateLimitCheckWithThresholdsIn applies the configured remaining-percent
// thresholds to one profile's statusline rate-limit update.
func RunRateLimitCheckWithThresholdsIn(
	stateDir string,
	stdinJSON string,
	nowMs int64,
	thresholds []int,
	notify ShowNotificationFn,
	percent core.LimitPercent,
) {
	rateLimits := ParseRateLimits(stdinJSON)
	if rateLimits == nil {
		return
	}
	if rateLimits.FiveHour == nil && rateLimits.SevenDay == nil {
		return
	}
	WithLockedStateIn(stateDir, func(current ClaudeNotifyState) ClaudeNotifyState {
		return ClaudeNotifyState{
			FiveHour: ProcessWindow(WindowFiveHour, rateLimits.FiveHour, current.FiveHour, nowMs, thresholds, notify, percent),
			SevenDay: ProcessWindow(WindowSevenDay, rateLimits.SevenDay, current.SevenDay, nowMs, thresholds, notify, percent),
		}
	})
}
