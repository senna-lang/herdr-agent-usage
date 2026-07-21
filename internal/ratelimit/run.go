/**
 * Reads rate_limits from the statusLine stdin and sends notifications when
 * a window crosses a bucket threshold (5h and 7d).
 */
package ratelimit

// ShowNotificationFn displays a toast; returns whether it was shown.
type ShowNotificationFn func(title, body string) bool

// ProcessWindow applies decideBucket + notify for one Claude window.
func ProcessWindow(
	key WindowKey,
	input *RateWindowInput,
	previous *WindowState,
	nowMs int64,
	notify ShowNotificationFn,
) *WindowState {
	if input == nil {
		return previous
	}
	decision := DecideBucket(WindowInput{
		UsedPercentage: input.UsedPercentage,
		ResetsAt:       input.ResetsAt,
	}, previous)
	if decision.BucketToNotify == nil {
		next := decision.NewState
		return &next
	}
	text := FormatNotificationBody(key, *decision.BucketToNotify, input.ResetsAt, nowMs)
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
	rateLimits := ParseRateLimits(stdinJSON)
	if rateLimits == nil {
		return
	}
	if rateLimits.FiveHour == nil && rateLimits.SevenDay == nil {
		return
	}
	WithLockedStateIn(stateDir, func(current ClaudeNotifyState) ClaudeNotifyState {
		return ClaudeNotifyState{
			FiveHour: ProcessWindow(WindowFiveHour, rateLimits.FiveHour, current.FiveHour, nowMs, notify),
			SevenDay: ProcessWindow(WindowSevenDay, rateLimits.SevenDay, current.SevenDay, nowMs, notify),
		}
	})
}
