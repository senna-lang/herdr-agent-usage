package limits

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

func allWindowsFixture() ProviderLimits {
	five := 300
	seven := 10080
	thirty := 43200
	return ProviderLimits{
		Primary:   &LimitWindow{UsedPercentage: 28.4, WindowMinutes: &five},
		Secondary: &LimitWindow{UsedPercentage: 70, WindowMinutes: &seven},
		Tertiary:  &LimitWindow{UsedPercentage: 62.9, WindowMinutes: &thirty},
	}
}

func TestFormatSidebarLimit_AllWindowsMode(t *testing.T) {
	SetSidebarWindows("all")
	t.Cleanup(func() { SetSidebarWindows("") })

	p := allWindowsFixture()
	if got, want := FormatSidebarLimit(p, sidebarNowMs, ""), "5h 72% · 7d 30% · 30d 37%"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := FormatSidebarLimit(p, sidebarNowMs, core.LimitPercentUsed), "5h 28% · 7d 70% · 30d 63%"; got != want {
		t.Fatalf("used mode: got %q, want %q", got, want)
	}
}

func TestFormatSidebarLimit_CompactMode(t *testing.T) {
	SetSidebarWindows("compact")
	t.Cleanup(func() { SetSidebarWindows("") })

	p := allWindowsFixture()
	if got, want := FormatSidebarLimit(p, sidebarNowMs, ""), "72% · 30% · 37%"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := FormatSidebarLimit(p, sidebarNowMs, core.LimitPercentUsed), "28% · 70% · 63%"; got != want {
		t.Fatalf("used mode: got %q, want %q", got, want)
	}
}

func TestFormatSidebarLimit_MultiWindowModesSkipAbsentWindows(t *testing.T) {
	p := ProviderLimits{Secondary: &LimitWindow{UsedPercentage: 41.6}}

	for _, mode := range []string{"all", "compact"} {
		SetSidebarWindows(mode)
		want := "7d 58%"
		if mode == "compact" {
			want = "58%"
		}
		if got := FormatSidebarLimit(p, sidebarNowMs, ""); got != want {
			t.Fatalf("%s: got %q, want %q", mode, got, want)
		}
		if got := FormatSidebarLimit(ProviderLimits{}, sidebarNowMs, ""); got != "" {
			t.Fatalf("%s: no windows: got %q, want empty", mode, got)
		}
	}
	SetSidebarWindows("")
}

func TestFormatSidebarLimit_CompactIncludesCountdown(t *testing.T) {
	SetSidebarWindows("compact")
	t.Cleanup(func() { SetSidebarWindows("") })

	five := 300
	seven := 10080
	thirty := 43200
	nowSec := sidebarNowMs / 1000
	p := ProviderLimits{
		Primary:   &LimitWindow{UsedPercentage: 28.4, WindowMinutes: &five, ResetsAt: ptr(nowSec + 3*3600)},
		Secondary: &LimitWindow{UsedPercentage: 70, WindowMinutes: &seven, ResetsAt: ptr(nowSec + 3*86400)},
		Tertiary:  &LimitWindow{UsedPercentage: 62.9, WindowMinutes: &thirty, ResetsAt: ptr(nowSec + 12*86400)},
	}
	if got, want := FormatSidebarLimit(p, sidebarNowMs, ""), "72% 3h · 30% 3d · 37% 12d"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func ptr(v int64) *int64 { return &v }

func TestFormatSidebarLimit_UnknownModeFallsBackToShortest(t *testing.T) {
	SetSidebarWindows("nonsense")
	t.Cleanup(func() { SetSidebarWindows("") })

	if got, want := FormatSidebarLimit(allWindowsFixture(), sidebarNowMs, ""), "5h 72%"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
