/**
 * Run-out projection (approach B): given a recently observed consumption
 * rate (%/min, from usage-history), estimate when a window empties and
 * whether that lands before its reset.
 *
 * Unlike a single-snapshot estimate, this cannot be fooled by rolling-window
 * reset semantics or by the uniform-vs-burst ambiguity: the rate is measured
 * from the derivative of used% over time, so a finished burst (idle now)
 * yields a non-positive rate and stays silent.
 */
package limits

import "math"

// ProjectRunOut projects run-out from a positive per-minute rate.
// Returns nil when we cannot make a meaningful "before reset" statement
// (no reset time, no positive rate) and the window is not already empty.
func ProjectRunOut(w LimitWindow, ratePerMin *float64, nowMs int64) *RunOutEstimate {
	used := math.Max(0, math.Min(100, w.UsedPercentage))
	remaining := 100 - used
	// Already empty is a fact, not an extrapolation.
	if remaining <= 0 {
		return &RunOutEstimate{MinutesToEmpty: 0, EmptyBeforeReset: true}
	}

	if ratePerMin == nil || *ratePerMin <= 0 {
		return nil
	}
	if w.ResetsAt == nil || *w.ResetsAt <= 0 {
		return nil
	}

	minutesToEmpty := remaining / *ratePerMin
	timeToResetMin := (float64(*w.ResetsAt)*1000 - float64(nowMs)) / 60_000
	emptyBeforeReset := minutesToEmpty < timeToResetMin

	return &RunOutEstimate{
		MinutesToEmpty:   minutesToEmpty,
		EmptyBeforeReset: emptyBeforeReset,
	}
}
