/**
 * Tests for ProjectRunOut (approach B).
 */
package limits

import (
	"math"
	"testing"
)

const burnNow int64 = 1_700_000_000_000

func windowAt(used float64, toResetMin float64) LimitWindow {
	resetsAt := int64(math.Round(float64(burnNow)/1000 + toResetMin*60))
	return LimitWindow{UsedPercentage: used, ResetsAt: &resetsAt}
}

func TestProjectRunOut_NullRate(t *testing.T) {
	if got := ProjectRunOut(windowAt(40, 120), nil, burnNow); got != nil {
		t.Fatalf("got %#v", got)
	}
}

func TestProjectRunOut_NonPositiveRate(t *testing.T) {
	zero := 0.0
	neg := -0.5
	if ProjectRunOut(windowAt(40, 120), &zero, burnNow) != nil {
		t.Fatal("expected nil for 0 rate")
	}
	if ProjectRunOut(windowAt(40, 120), &neg, burnNow) != nil {
		t.Fatal("expected nil for negative rate")
	}
}

func TestProjectRunOut_NoReset(t *testing.T) {
	rate := 1.0
	if ProjectRunOut(LimitWindow{UsedPercentage: 40}, &rate, burnNow) != nil {
		t.Fatal("expected nil without resetsAt")
	}
}

func TestProjectRunOut_AlreadyEmpty(t *testing.T) {
	got := ProjectRunOut(windowAt(100, 120), nil, burnNow)
	if got == nil || got.MinutesToEmpty != 0 || !got.EmptyBeforeReset {
		t.Fatalf("got %#v", got)
	}
}

func TestProjectRunOut_FastEmptiesBeforeReset(t *testing.T) {
	rate := 1.0
	est := ProjectRunOut(windowAt(40, 120), &rate, burnNow)
	if est == nil || !est.EmptyBeforeReset {
		t.Fatalf("got %#v", est)
	}
	if math.Abs(est.MinutesToEmpty-60) > 1e-5 {
		t.Fatalf("minutesToEmpty=%v want ~60", est.MinutesToEmpty)
	}
}

func TestProjectRunOut_SlowHoldsUntilReset(t *testing.T) {
	rate := 0.1
	est := ProjectRunOut(windowAt(40, 120), &rate, burnNow)
	if est == nil || est.EmptyBeforeReset {
		t.Fatalf("got %#v", est)
	}
}
