package limits

import "testing"

func TestMostConstrainedRemaining(t *testing.T) {
	provider := ProviderLimits{
		Primary:   &LimitWindow{UsedPercentage: 10},
		Secondary: &LimitWindow{UsedPercentage: 89.4},
		Tertiary:  &LimitWindow{UsedPercentage: 40},
	}
	got, ok := MostConstrainedRemaining(provider)
	if !ok || got != 11 {
		t.Fatalf("got=%d ok=%v, want 11 true", got, ok)
	}
}

func TestMostConstrainedRemainingNoWindows(t *testing.T) {
	if got, ok := MostConstrainedRemaining(ProviderLimits{}); ok || got != 101 {
		t.Fatalf("got=%d ok=%v, want no value", got, ok)
	}
}

func TestWindowRemaining(t *testing.T) {
	got, ok := WindowRemaining(&LimitWindow{UsedPercentage: 0.4})
	if !ok || got != 100 {
		t.Fatalf("got=%d ok=%v, want 100 true", got, ok)
	}
	if _, ok := WindowRemaining(nil); ok {
		t.Fatal("nil window unexpectedly returned a value")
	}
}
