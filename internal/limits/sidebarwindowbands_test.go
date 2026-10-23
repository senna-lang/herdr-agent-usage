package limits

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

func TestFormatSidebarWindowBands(t *testing.T) {
	five := 300
	seven := 10080
	thirty := 43200
	p := ProviderLimits{
		Primary:   &LimitWindow{UsedPercentage: 4, WindowMinutes: &five},
		Secondary: &LimitWindow{UsedPercentage: 3, WindowMinutes: &seven},
		Tertiary:  &LimitWindow{UsedPercentage: 63, WindowMinutes: &thirty},
	}

	bands := FormatSidebarWindowBands(p, "", sidebarNowMs)
	want := []SidebarWindowBand{
		{Tag: "5h", Text: "96%", Band: core.LimitHigh},
		{Tag: "7d", Text: "97%", Band: core.LimitHigh},
		{Tag: "30d", Text: "37%", Band: core.LimitMid},
	}
	if len(bands) != len(want) {
		t.Fatalf("got %d bands, want %d: %+v", len(bands), len(want), bands)
	}
	for i := range want {
		if bands[i] != want[i] {
			t.Errorf("band %d = %+v, want %+v", i, bands[i], want[i])
		}
	}
}

func TestFormatSidebarWindowBandsSkipsAbsentAndClamps(t *testing.T) {
	seven := 10080
	p := ProviderLimits{
		Secondary: &LimitWindow{UsedPercentage: 90, WindowMinutes: &seven},
	}
	bands := FormatSidebarWindowBands(p, core.LimitPercentUsed, sidebarNowMs)
	if len(bands) != 1 {
		t.Fatalf("got %+v, want one band", bands)
	}
	if bands[0].Text != "90%" {
		t.Errorf("text = %q, want %q", bands[0].Text, "90%")
	}
	if bands[0].Band != core.LimitLow {
		t.Errorf("band = %q, want low", bands[0].Band)
	}

	over := ProviderLimits{Primary: &LimitWindow{UsedPercentage: 140}}
	bands = FormatSidebarWindowBands(over, "", sidebarNowMs)
	if len(bands) != 1 || bands[0].Text != "0%" || bands[0].Band != core.LimitLow {
		t.Fatalf("over-limit: got %+v, want 0%% low", bands)
	}
}
