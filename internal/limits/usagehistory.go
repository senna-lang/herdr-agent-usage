/**
 * Recent-pace tracking for run-out projection (approach B).
 *
 * A single snapshot cannot tell "60% used evenly over 5h" from "60% in the
 * last 10 min" — both carry identical (used%, resetsAt, windowMinutes). To
 * detect a burst we need the derivative of used% over time, so we persist a
 * short series of (t, used%) samples per provider window and take the slope
 * over a bounded recent lookback.
 *
 * All functions here are pure; persistence lives in the I/O adapter layer.
 */
package limits

import "math"

// UsageSample is one (t, used%) observation.
type UsageSample struct {
	// T is sample time (epoch ms).
	T int64
	// Used percentage (0-100) at that time.
	Used float64
}

// UsageHistory is keyed by `${providerId}:${kind}` (kind = "primary" | "secondary").
type UsageHistory map[string][]UsageSample

// HistoryOptions controls sample retention.
type HistoryOptions struct {
	// HorizonMs drops samples older than this before now (prune horizon).
	HorizonMs int64
	// MinGapMs ignores a new sample if the last one is newer than this (dedupe).
	MinGapMs int64
	// MaxSamples is a hard cap on retained samples per key.
	MaxSamples int
}

// DefaultHistoryOptions are the production defaults.
var DefaultHistoryOptions = HistoryOptions{
	HorizonMs:  90 * 60_000,
	MinGapMs:   5_000,
	MaxSamples: 512,
}

// RateOptions controls recentRatePerMin slope fitting.
type RateOptions struct {
	// LookbackMs: only samples within this lookback from now feed the slope.
	LookbackMs int64
	// MinSpanMs: require at least this time span between first and last sample.
	MinSpanMs int64
	// MinSamples: require at least this many samples.
	MinSamples int
}

// DefaultRateOptions are the production defaults.
var DefaultRateOptions = RateOptions{
	LookbackMs: 20 * 60_000,
	MinSpanMs:  3 * 60_000,
	MinSamples: 3,
}

// RecordSample appends a sample to key's series and prunes anything older than
// the horizon. Skips the append when the previous sample is within MinGapMs.
func RecordSample(history UsageHistory, key string, sample UsageSample, opts HistoryOptions) UsageHistory {
	if !isFinite(sample.Used) {
		return history
	}
	prev := history[key]
	var last *UsageSample
	if len(prev) > 0 {
		last = &prev[len(prev)-1]
	}
	cutoff := sample.T - opts.HorizonMs

	kept := make([]UsageSample, 0, len(prev)+1)
	for _, s := range prev {
		if s.T >= cutoff && s.T <= sample.T {
			kept = append(kept, s)
		}
	}
	skip := last != nil && sample.T-last.T < opts.MinGapMs
	next := kept
	if !skip {
		next = append(kept, UsageSample{T: sample.T, Used: sample.Used})
	}
	if opts.MaxSamples > 0 && len(next) > opts.MaxSamples {
		next = next[len(next)-opts.MaxSamples:]
	}

	out := make(UsageHistory, len(history)+1)
	for k, v := range history {
		out[k] = v
	}
	out[key] = next
	return out
}

// RecentRatePerMin returns least-squares slope of used% over time (%/min)
// across the recent lookback. Returns nil when there is not enough recent
// data, or when the pace is flat/negative (idle or a rolling window freeing up).
func RecentRatePerMin(samples []UsageSample, nowMs int64, opts RateOptions) *float64 {
	if samples == nil {
		return nil
	}
	cutoff := nowMs - opts.LookbackMs
	recent := make([]UsageSample, 0, len(samples))
	for _, s := range samples {
		if s.T >= cutoff && s.T <= nowMs {
			recent = append(recent, s)
		}
	}
	if len(recent) < opts.MinSamples {
		return nil
	}
	first := recent[0]
	last := recent[len(recent)-1]
	spanMs := last.T - first.T
	if spanMs < opts.MinSpanMs {
		return nil
	}

	n := float64(len(recent))
	var sumT, sumU, sumTT, sumTU float64
	for _, s := range recent {
		t := float64(s.T - first.T) // shift for numerical stability
		sumT += t
		sumU += s.Used
		sumTT += t * t
		sumTU += t * s.Used
	}
	denom := n*sumTT - sumT*sumT
	if denom == 0 {
		return nil
	}
	slopePerMs := (n*sumTU - sumT*sumU) / denom
	slopePerMin := slopePerMs * 60_000
	if math.IsNaN(slopePerMin) || math.IsInf(slopePerMin, 0) || slopePerMin <= 0 {
		return nil
	}
	return &slopePerMin
}
