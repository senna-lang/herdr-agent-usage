/**
 * Ties the rate and projection pieces together and attaches a `runOut`
 * estimate to each window. The rate is chosen by window length:
 *
 *   - Short window (session, <= ShortWindowMaxMin): the recent
 *     instantaneous pace from history (approach B) — catches a burst you're
 *     doing right now.
 *   - Long window (weekly / monthly): elapsed-average
 *     `used% / elapsedMinutes` where elapsed is inferred from
 *     windowMinutes − timeToReset. A min-elapsed floor dampens the rate
 *     right after reset so a tiny numerator does not explode the pace.
 *     Needs no history.
 *
 * Pure: takes the previous history in and returns the next history out, so
 * persistence (IO) lives entirely in the caller (the pane loop).
 */
package limits

import "math"

// RunOutOptions bundles history + rate options for EnrichRunOut.
type RunOutOptions struct {
	History HistoryOptions
	Rate    RateOptions
}

// DefaultRunOutOptions are production defaults.
var DefaultRunOutOptions = RunOutOptions{
	History: DefaultHistoryOptions,
	Rate:    DefaultRateOptions,
}

// ShortWindowMaxMin: windows at or below this length use the instantaneous recent rate.
const ShortWindowMaxMin = 360

// LongWindowMinElapsedMin floors elapsed minutes for long-window pace.
// Prevents "5% used in the first 10 minutes" from projecting an absurd rate.
// 12h sits between "a few hours" and "one day".
const LongWindowMinElapsedMin = 12 * 60

type windowKind string

const (
	kindPrimary   windowKind = "primary"
	kindSecondary windowKind = "secondary"
	kindTertiary  windowKind = "tertiary"
)

func keyFor(providerID string, kind windowKind) string {
	return providerID + ":" + string(kind)
}

// ElapsedAverageRatePerMin is the long-window pace (%/min): used% divided by
// minutes elapsed since the window started (inferred from ResetsAt), not by
// the full window length.
//
// elapsed = clamp(windowMinutes − timeToReset, minElapsed, windowMinutes)
// When ResetsAt is missing, falls back to used% / windowMinutes.
func ElapsedAverageRatePerMin(w LimitWindow, nowMs int64, minElapsedMin int) *float64 {
	if w.WindowMinutes == nil || *w.WindowMinutes <= 0 {
		return nil
	}
	used := math.Max(0, math.Min(100, w.UsedPercentage))
	if used <= 0 {
		return nil
	}

	windowMin := float64(*w.WindowMinutes)
	floor := float64(minElapsedMin)
	if floor < 1 {
		floor = 1
	}
	if floor > windowMin {
		floor = windowMin
	}

	if w.ResetsAt == nil || *w.ResetsAt <= 0 {
		rate := used / windowMin
		return &rate
	}

	timeToResetMin := float64(*w.ResetsAt*1000-nowMs) / 60_000
	rawElapsed := windowMin - timeToResetMin
	elapsed := math.Min(windowMin, math.Max(floor, rawElapsed))
	rate := used / elapsed
	return &rate
}

// EnrichRunOutResult is the enriched providers plus updated history.
type EnrichRunOutResult struct {
	Providers []ProviderLimits
	History   UsageHistory
}

// EnrichRunOut records samples for short windows, then returns the providers
// with runOut attached and the updated history.
func EnrichRunOut(
	providers []ProviderLimits,
	prevHistory UsageHistory,
	nowMs int64,
	opts RunOutOptions,
) EnrichRunOutResult {
	history := prevHistory
	if history == nil {
		history = UsageHistory{}
	}

	withRunOut := func(providerID string, kind windowKind, w *LimitWindow) *LimitWindow {
		if w == nil {
			return nil
		}
		// copy so we don't mutate caller's window
		next := *w

		isLong := w.WindowMinutes != nil && *w.WindowMinutes > ShortWindowMaxMin

		var rate *float64
		if isLong {
			rate = ElapsedAverageRatePerMin(*w, nowMs, LongWindowMinElapsedMin)
		} else {
			key := keyFor(providerID, kind)
			history = RecordSample(history, key, UsageSample{T: nowMs, Used: w.UsedPercentage}, opts.History)
			rate = RecentRatePerMin(history[key], nowMs, opts.Rate)
		}

		// Only attach when it actually warns, so runOut present ⇒ a warning.
		runOut := ProjectRunOut(*w, rate, nowMs)
		if runOut != nil && runOut.EmptyBeforeReset {
			next.RunOut = runOut
		}
		return &next
	}

	nextProviders := make([]ProviderLimits, len(providers))
	for i, p := range providers {
		nextProviders[i] = ProviderLimits{
			ProviderID:   p.ProviderID,
			Label:        p.Label,
			Primary:      withRunOut(p.ProviderID, kindPrimary, p.Primary),
			Secondary:    withRunOut(p.ProviderID, kindSecondary, p.Secondary),
			Tertiary:     withRunOut(p.ProviderID, kindTertiary, p.Tertiary),
			PlanType:     p.PlanType,
			Source:       p.Source,
			FetchedAtMs:  p.FetchedAtMs,
			Note:         p.Note,
			PaneActivity: p.PaneActivity,
		}
	}

	return EnrichRunOutResult{Providers: nextProviders, History: history}
}
