package limits

import "testing"

const sidebarNowMs int64 = 1_800_000_000_000

func TestFormatSidebarLimit_PrefersShortestAvailableWindow(t *testing.T) {
	five := 300
	seven := 10080
	p := ProviderLimits{
		Primary:   &LimitWindow{UsedPercentage: 28.4, WindowMinutes: &five},
		Secondary: &LimitWindow{UsedPercentage: 70, WindowMinutes: &seven},
	}
	if got := FormatSidebarLimit(p, sidebarNowMs); got != "5h 72%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatSidebarLimit_FallsBackToNextWindow(t *testing.T) {
	seven := 10080
	p := ProviderLimits{Secondary: &LimitWindow{UsedPercentage: 41.6, WindowMinutes: &seven}}
	if got := FormatSidebarLimit(p, sidebarNowMs); got != "7d 58%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatSidebarLimit_NoWindows(t *testing.T) {
	if got := FormatSidebarLimit(ProviderLimits{}, sidebarNowMs); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatSidebarLimit_SkipsExpiredWindow(t *testing.T) {
	five := 300
	seven := 10080
	expired := sidebarNowMs / 1000
	future := expired + 3600
	p := ProviderLimits{
		Primary: &LimitWindow{
			UsedPercentage: 97,
			WindowMinutes:  &five,
			ResetsAt:       &expired,
		},
		Secondary: &LimitWindow{
			UsedPercentage: 42,
			WindowMinutes:  &seven,
			ResetsAt:       &future,
		},
	}
	if got := FormatSidebarLimit(p, sidebarNowMs); got != "7d 58%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatSidebarLimit_AllWindowsExpired(t *testing.T) {
	expired := sidebarNowMs/1000 - 1
	p := ProviderLimits{Primary: &LimitWindow{UsedPercentage: 97, ResetsAt: &expired}}
	if got := FormatSidebarLimit(p, sidebarNowMs); got != "" {
		t.Fatalf("got %q", got)
	}
}
