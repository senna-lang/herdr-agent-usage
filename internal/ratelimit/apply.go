/**
 * Pure function that decides how a notification's delivery result maps to
 * the confirmed state.
 */
package ratelimit

// ApplyNotifyResult confirms the candidate only when shown=true.
// When the notification could not be shown we keep previous so the next
// bucket decision can retry. After MaxFailedNotifyAttempts failures we give
// up on the toast and confirm the candidate.
func ApplyNotifyResult(previous *WindowState, candidate WindowState, shown bool) *WindowState {
	if shown {
		next := candidate
		next.FailedNotifyAttempts = 0
		return &next
	}

	attempts := 1
	if previous != nil {
		attempts = previous.FailedNotifyAttempts + 1
	}
	if attempts >= MaxFailedNotifyAttempts {
		next := candidate
		next.FailedNotifyAttempts = 0
		return &next
	}

	if previous == nil {
		return &WindowState{
			ResetsAt:             candidate.ResetsAt,
			NotifiedBucket:       nil,
			FailedNotifyAttempts: attempts,
		}
	}
	next := *previous
	next.FailedNotifyAttempts = attempts
	return &next
}
