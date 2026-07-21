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

func TestFormatSidebarBurn_TokensOnly(t *testing.T) {
	cases := []struct {
		tokens float64
		want   string
	}{
		{0, ""},
		{0.4, ""},
		{812, "Σ 812"},
		{9_540, "Σ 9.5k"},
		{350_412, "Σ 350k"},
		{5_749_740, "Σ 5.7M"},
		{12_400_000, "Σ 12M"},
	}
	for _, c := range cases {
		if got := FormatSidebarBurn(c.tokens, 0); got != c.want {
			t.Fatalf("FormatSidebarBurn(%v,0)=%q want %q", c.tokens, got, c.want)
		}
	}
}

func TestFormatSidebarBurn_WithCost(t *testing.T) {
	cases := []struct {
		tokens, cost float64
		want         string
	}{
		{74_000, 0.0078, "Σ 74k $0.0078"},
		{350_412, 0.42, "Σ 350k $0.42"},
		{5_749_740, 128.5, "Σ 5.7M $129"},
		// Zero cost (uncataloged model, e.g. local Ollama) omits the segment
		// rather than printing a misleading "$0.00".
		{1_371_000, 0, "Σ 1.4M"},
	}
	for _, c := range cases {
		if got := FormatSidebarBurn(c.tokens, c.cost); got != c.want {
			t.Fatalf("FormatSidebarBurn(%v,%v)=%q want %q", c.tokens, c.cost, got, c.want)
		}
	}
}

func TestFormatBurnCost_Tiers(t *testing.T) {
	cases := []struct {
		usd  float64
		want string
	}{
		{0.0078, "$0.0078"},
		{0.42, "$0.42"},
		{9.999, "$10.00"},
		{128.5, "$129"},
	}
	for _, c := range cases {
		if got := formatCompactCost(c.usd); got != c.want {
			t.Fatalf("formatCompactCost(%v)=%q want %q", c.usd, got, c.want)
		}
	}
}
