/**
 * Tests for EnrichRunOut — the window-length-aware rate + projection wiring.
 *
 * Short windows (primary) use the recent instantaneous pace from history;
 * long windows (secondary/tertiary) use elapsed-average used%/elapsed
 * (with a min-elapsed floor), not used%/full-window.
 */
package limits

import (
	"math"
	"testing"
)

const (
	runMin = int64(60_000)
	runNow = int64(1_700_000_000_000)
	day    = 24 * 60
	week   = 7 * day
)

var runOpts = RunOutOptions{
	History: HistoryOptions{HorizonMs: 90 * runMin, MinGapMs: 0, MaxSamples: 512},
	Rate:    RateOptions{LookbackMs: 20 * runMin, MinSpanMs: 3 * runMin, MinSamples: 3},
}

func testProvider(primary, secondary *LimitWindow) ProviderLimits {
	return ProviderLimits{
		ProviderID:  "p",
		Label:       "P",
		Source:      "test",
		FetchedAtMs: runNow,
		Primary:     primary,
		Secondary:   secondary,
	}
}

func feedPrimaryRamp(slopePerMin, endUsed float64) ProviderLimits {
	history := UsageHistory{}
	var providers []ProviderLimits
	count := 11
	stepMin := 1.0
	for i := count - 1; i >= 0; i-- {
		t := runNow - int64(float64(i)*stepMin*float64(runMin))
		used := endUsed - slopePerMin*float64(i)*stepMin
		wm := 300
		resetsAt := int64(math.Round(float64(runNow)/1000 + 120*60))
		p := testProvider(&LimitWindow{
			UsedPercentage: used,
			WindowMinutes:  &wm,
			ResetsAt:       &resetsAt,
		}, nil)
		res := EnrichRunOut([]ProviderLimits{p}, history, t, runOpts)
		providers = res.Providers
		history = res.History
	}
	return providers[0]
}

func TestEnrichRunOut_ShortWindow_WarnsOnBurst(t *testing.T) {
	p := feedPrimaryRamp(2, 80)
	if p.Primary == nil || p.Primary.RunOut == nil || !p.Primary.RunOut.EmptyBeforeReset {
		t.Fatalf("primary runOut=%#v", p.Primary)
	}
}

func TestEnrichRunOut_ShortWindow_SilentWhenIdle(t *testing.T) {
	p := feedPrimaryRamp(0, 60)
	if p.Primary != nil && p.Primary.RunOut != nil {
		t.Fatalf("expected no runOut, got %#v", p.Primary.RunOut)
	}
}

func TestEnrichRunOut_ShortWindow_ColdStart(t *testing.T) {
	wm := 300
	resetsAt := int64(math.Round(float64(runNow)/1000 + 120*60))
	p := testProvider(&LimitWindow{
		UsedPercentage: 60,
		WindowMinutes:  &wm,
		ResetsAt:       &resetsAt,
	}, nil)
	res := EnrichRunOut([]ProviderLimits{p}, UsageHistory{}, runNow, runOpts)
	if res.Providers[0].Primary != nil && res.Providers[0].Primary.RunOut != nil {
		t.Fatalf("expected no runOut on cold start")
	}
}

func TestElapsedAverageRatePerMin_FrontLoadedOneDayOfSeven(t *testing.T) {
	// 1 day into a 7d window: 60% used → rate = 60%/day, not 60%/7d.
	wm := week
	resetsAt := int64(math.Round(float64(runNow)/1000 + float64(6*day)*60))
	w := LimitWindow{
		UsedPercentage: 60,
		WindowMinutes:  &wm,
		ResetsAt:       &resetsAt,
	}
	rate := ElapsedAverageRatePerMin(w, runNow, LongWindowMinElapsedMin)
	if rate == nil {
		t.Fatal("expected rate")
	}
	want := 60.0 / float64(day)
	if math.Abs(*rate-want) > 1e-8 {
		t.Fatalf("rate=%v want %v", *rate, want)
	}
}

func TestElapsedAverageRatePerMin_ClampsUpToMinElapsed(t *testing.T) {
	// Only 30 minutes into the window — without a floor, rate would explode.
	wm := week
	resetsAt := int64(math.Round(float64(runNow)/1000 + float64(week-30)*60))
	w := LimitWindow{
		UsedPercentage: 10,
		WindowMinutes:  &wm,
		ResetsAt:       &resetsAt,
	}
	rate := ElapsedAverageRatePerMin(w, runNow, LongWindowMinElapsedMin)
	if rate == nil {
		t.Fatal("expected rate")
	}
	want := 10.0 / float64(LongWindowMinElapsedMin)
	if math.Abs(*rate-want) > 1e-8 {
		t.Fatalf("rate=%v want %v", *rate, want)
	}
}

func TestElapsedAverageRatePerMin_ClampsDownToWindowWhenPastReset(t *testing.T) {
	wm := week
	resetsAt := int64(math.Round(float64(runNow)/1000 - 60)) // already past
	w := LimitWindow{
		UsedPercentage: 40,
		WindowMinutes:  &wm,
		ResetsAt:       &resetsAt,
	}
	rate := ElapsedAverageRatePerMin(w, runNow, LongWindowMinElapsedMin)
	if rate == nil {
		t.Fatal("expected rate")
	}
	want := 40.0 / float64(week)
	if math.Abs(*rate-want) > 1e-8 {
		t.Fatalf("rate=%v want %v", *rate, want)
	}
}

func TestElapsedAverageRatePerMin_FallbackWithoutResetsAt(t *testing.T) {
	wm := week
	w := LimitWindow{
		UsedPercentage: 50,
		WindowMinutes:  &wm,
	}
	rate := ElapsedAverageRatePerMin(w, runNow, LongWindowMinElapsedMin)
	if rate == nil {
		t.Fatal("expected rate")
	}
	want := 50.0 / float64(week)
	if math.Abs(*rate-want) > 1e-8 {
		t.Fatalf("rate=%v want %v", *rate, want)
	}
}

func TestElapsedAverageRatePerMin_NullWhenUsedZeroOrInvalidWindow(t *testing.T) {
	wm := week
	resetsAt := int64(math.Round(float64(runNow)/1000 + float64(week)*60))
	if ElapsedAverageRatePerMin(LimitWindow{
		UsedPercentage: 0,
		WindowMinutes:  &wm,
		ResetsAt:       &resetsAt,
	}, runNow, LongWindowMinElapsedMin) != nil {
		t.Fatal("expected nil for used=0")
	}
	zero := 0
	if ElapsedAverageRatePerMin(LimitWindow{
		UsedPercentage: 10,
		WindowMinutes:  &zero,
	}, runNow, LongWindowMinElapsedMin) != nil {
		t.Fatal("expected nil for invalid window")
	}
}

func TestEnrichRunOut_LongWindow_FrontLoadedWarnsAboutPointSevenDay(t *testing.T) {
	// Grok-style: heavy day early in a 7d credit cycle → ~0.7d empty, not ~4d.
	wm := week
	resetsAt := int64(math.Round(float64(runNow)/1000 + float64(6*day)*60))
	p := testProvider(nil, &LimitWindow{
		UsedPercentage: 60,
		WindowMinutes:  &wm,
		ResetsAt:       &resetsAt,
	})
	res := EnrichRunOut([]ProviderLimits{p}, UsageHistory{}, runNow, runOpts)
	run := res.Providers[0].Secondary.RunOut
	if run == nil || !run.EmptyBeforeReset {
		t.Fatalf("runOut=%#v", run)
	}
	// remaining 40% at 60%/day → (40/60)*DAY minutes
	want := (40.0 / 60.0) * float64(day)
	if math.Abs(run.MinutesToEmpty-want) > 1 {
		t.Fatalf("minutesToEmpty=%v want ~%v", run.MinutesToEmpty, want)
	}
}

func TestEnrichRunOut_LongWindow_EvenPaceNearEndHolds(t *testing.T) {
	// 20% used after ~6.5d, 0.5d left — pace far below emptying before reset.
	wm := week
	resetsAt := int64(math.Round(float64(runNow)/1000 + 0.5*float64(day)*60))
	p := testProvider(nil, &LimitWindow{
		UsedPercentage: 20,
		WindowMinutes:  &wm,
		ResetsAt:       &resetsAt,
	})
	res := EnrichRunOut([]ProviderLimits{p}, UsageHistory{}, runNow, runOpts)
	if res.Providers[0].Secondary != nil && res.Providers[0].Secondary.RunOut != nil {
		t.Fatalf("expected no warning, got %#v", res.Providers[0].Secondary.RunOut)
	}
}

func TestEnrichRunOut_LongWindow_NoHistoryRecorded(t *testing.T) {
	wm := week
	resetsAt := int64(math.Round(float64(runNow)/1000 + float64(6*day)*60))
	p := testProvider(nil, &LimitWindow{
		UsedPercentage: 58,
		WindowMinutes:  &wm,
		ResetsAt:       &resetsAt,
	})
	res := EnrichRunOut([]ProviderLimits{p}, UsageHistory{}, runNow, runOpts)
	if len(res.History) != 0 {
		t.Fatalf("history keys=%v", res.History)
	}
}
