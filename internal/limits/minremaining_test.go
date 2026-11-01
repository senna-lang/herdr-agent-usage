package limits

import "testing"

func TestMinRemainingPercent(t *testing.T) {
	five := 300
	seven := 10080
	thirty := 43200
	p := ProviderLimits{
		Primary:   &LimitWindow{UsedPercentage: 4, WindowMinutes: &five},
		Secondary: &LimitWindow{UsedPercentage: 3, WindowMinutes: &seven},
		Tertiary:  &LimitWindow{UsedPercentage: 63, WindowMinutes: &thirty},
	}
	if got, want := MinRemainingPercent(p), 37.0; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}

	if got := MinRemainingPercent(ProviderLimits{}); got != -1 {
		t.Fatalf("empty provider: got %v, want -1", got)
	}

	over := ProviderLimits{Primary: &LimitWindow{UsedPercentage: 140}}
	if got := MinRemainingPercent(over); got != 0 {
		t.Fatalf("over-limit: got %v, want 0", got)
	}
}
