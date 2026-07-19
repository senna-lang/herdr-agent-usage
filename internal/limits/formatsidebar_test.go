package limits

import "testing"

func TestFormatSidebarLimit_PrefersShortestAvailableWindow(t *testing.T) {
	five := 300
	seven := 10080
	p := ProviderLimits{
		Primary:   &LimitWindow{UsedPercentage: 28.4, WindowMinutes: &five},
		Secondary: &LimitWindow{UsedPercentage: 70, WindowMinutes: &seven},
	}
	if got := FormatSidebarLimit(p); got != "5h 72%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatSidebarLimit_FallsBackToNextWindow(t *testing.T) {
	seven := 10080
	p := ProviderLimits{Secondary: &LimitWindow{UsedPercentage: 41.6, WindowMinutes: &seven}}
	if got := FormatSidebarLimit(p); got != "7d 58%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatSidebarLimit_NoWindows(t *testing.T) {
	if got := FormatSidebarLimit(ProviderLimits{}); got != "" {
		t.Fatalf("got %q", got)
	}
}
