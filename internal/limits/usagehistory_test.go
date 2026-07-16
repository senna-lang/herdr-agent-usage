/**
 * Tests for usage-history (approach B recent-pace tracking).
 */
package limits

import (
	"math"
	"testing"
)

const (
	histMin = int64(60_000)
	histNow = int64(1_700_000_000_000)
)

var rateOpts = RateOptions{
	LookbackMs: 20 * histMin,
	MinSpanMs:  3 * histMin,
	MinSamples: 3,
}

// ramp builds a series where used% rises linearly at slopePerMin over count
// samples spaced stepMin apart, ending at histNow.
func ramp(startUsed, slopePerMin, stepMin float64, count int) []UsageSample {
	out := make([]UsageSample, 0, count)
	for i := count - 1; i >= 0; i-- {
		t := histNow - int64(float64(i)*stepMin*float64(histMin))
		minutesFromStart := float64(count-1-i) * stepMin
		out = append(out, UsageSample{
			T:    t,
			Used: startUsed + slopePerMin*minutesFromStart,
		})
	}
	return out
}

func TestRecordSample_AppendsAndPrunes(t *testing.T) {
	opts := HistoryOptions{HorizonMs: 10 * histMin, MinGapMs: 5_000, MaxSamples: 100}
	h := UsageHistory{}
	h = RecordSample(h, "k", UsageSample{T: histNow - 20*histMin, Used: 10}, opts)
	h = RecordSample(h, "k", UsageSample{T: histNow, Used: 40}, opts)
	if len(h["k"]) != 1 || h["k"][0].Used != 40 {
		t.Fatalf("got %#v", h["k"])
	}
}

func TestRecordSample_SkipsMinGap(t *testing.T) {
	opts := HistoryOptions{HorizonMs: 90 * histMin, MinGapMs: 5_000, MaxSamples: 100}
	h := UsageHistory{}
	h = RecordSample(h, "k", UsageSample{T: histNow, Used: 10}, opts)
	h = RecordSample(h, "k", UsageSample{T: histNow + 1_000, Used: 11}, opts)
	if len(h["k"]) != 1 {
		t.Fatalf("got %#v", h["k"])
	}
}

func TestRecordSample_CapsCount(t *testing.T) {
	opts := HistoryOptions{HorizonMs: 999 * histMin, MinGapMs: 0, MaxSamples: 3}
	h := UsageHistory{}
	for i := 0; i < 10; i++ {
		h = RecordSample(h, "k", UsageSample{T: histNow + int64(i)*10_000, Used: float64(i)}, opts)
	}
	if len(h["k"]) != 3 {
		t.Fatalf("got len=%d", len(h["k"]))
	}
}

func TestRecentRatePerMin_NotEnough(t *testing.T) {
	if RecentRatePerMin(nil, histNow, rateOpts) != nil {
		t.Fatal("expected nil")
	}
	if RecentRatePerMin([]UsageSample{{T: histNow, Used: 10}}, histNow, rateOpts) != nil {
		t.Fatal("expected nil")
	}
}

func TestRecentRatePerMin_SpanTooShort(t *testing.T) {
	samples := ramp(10, 1, 0.5, 3)
	if RecentRatePerMin(samples, histNow, rateOpts) != nil {
		t.Fatal("expected nil")
	}
}

func TestRecentRatePerMin_PositiveSlope(t *testing.T) {
	samples := ramp(20, 2, 1, 11)
	rate := RecentRatePerMin(samples, histNow, rateOpts)
	if rate == nil {
		t.Fatal("expected rate")
	}
	if math.Abs(*rate-2) > 0.15 {
		t.Fatalf("rate=%v want ~2", *rate)
	}
}

func TestRecentRatePerMin_Flat(t *testing.T) {
	samples := ramp(50, 0, 1, 11)
	if RecentRatePerMin(samples, histNow, rateOpts) != nil {
		t.Fatal("expected nil")
	}
}

func TestRecentRatePerMin_Falling(t *testing.T) {
	samples := ramp(60, -1, 1, 11)
	if RecentRatePerMin(samples, histNow, rateOpts) != nil {
		t.Fatal("expected nil")
	}
}

func TestRecentRatePerMin_IgnoresOldLookback(t *testing.T) {
	old := ramp(10, 3, 1, 5)
	for i := range old {
		old[i].T -= 40 * histMin
	}
	recentFlat := ramp(60, 0, 1, 6)
	samples := append(old, recentFlat...)
	if RecentRatePerMin(samples, histNow, rateOpts) != nil {
		t.Fatal("expected nil")
	}
}
