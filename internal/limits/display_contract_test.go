package limits

import (
	"strings"
	"testing"
)

func contractWindow(minutes int, used float64, resetInMinutes int64) *LimitWindow {
	reset := int64(1_800_000_000 + resetInMinutes*60)
	return &LimitWindow{UsedPercentage: used, WindowMinutes: &minutes, ResetsAt: &reset}
}

func TestUsagePaneSubscriptionContract_RendersEveryProviderWindowWithLeftAndReset(t *testing.T) {
	const nowMs int64 = 1_800_000_000_000
	tests := []struct {
		name     string
		provider ProviderLimits
		want     []string
	}{
		{
			name: "Claude 5h and 7d",
			provider: ProviderLimits{ProviderID: "claude", Label: "Claude",
				Primary: contractWindow(300, 13, 90), Secondary: contractWindow(10080, 79, 2*24*60)},
			want: []string{"Claude", "5h", "87% left", "1h 30m", "7d", "21% left", "2d 0h"},
		},
		{
			name: "Codex provider supplied 7d",
			provider: ProviderLimits{ProviderID: "codex", Label: "Codex",
				Primary: contractWindow(10080, 67, 3*24*60)},
			want: []string{"Codex", "7d", "33% left", "3d 0h"},
		},
		{
			name: "OpenCode Go 5h 7d and 30d",
			provider: ProviderLimits{ProviderID: "opencode", Label: "OpenCode Go",
				Primary: contractWindow(300, 0, 60), Secondary: contractWindow(10080, 20, 4*24*60), Tertiary: contractWindow(43200, 40, 10*24*60)},
			want: []string{"OpenCode Go", "5h", "100% left", "1h 0m", "7d", "80% left", "4d 0h", "30d", "60% left", "10d 0h"},
		},
		{
			name: "Grok Lite 7d",
			provider: ProviderLimits{ProviderID: "grok", Label: "Grok",
				Primary: contractWindow(10080, 86, 5*24*60)},
			want: []string{"Grok", "7d", "14% left", "5d 0h"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := FormatUsagePanel([]ProviderLimits{tt.provider}, nil, nowMs, PanelLayout{Columns: 100, Rows: 40})
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Fatalf("missing %q in:\n%s", want, out)
				}
			}
		})
	}
}

func TestUsagePaneProviderFirstContract_SubscriptionAcrossHarnessesIsOneBlock(t *testing.T) {
	provider := ProviderLimits{
		ProviderID: "opencode", Label: "OpenCode Go",
		Primary: contractWindow(300, 10, 60),
		PaneActivity: &ProviderPaneActivity{WindowMinutes: 300, TotalTokens: 1000, Panes: []PaneActivityShare{
			{PaneID: "omp", Label: "OMP", Tokens: 600, SharePercent: 60},
			{PaneID: "opencode", Label: "OpenCode", Tokens: 400, SharePercent: 40},
		}},
	}
	out := FormatUsagePanel([]ProviderLimits{provider}, nil, 1_800_000_000_000, PanelLayout{Columns: 100, Rows: 40})
	if strings.Count(out, "OpenCode Go") != 1 {
		t.Fatalf("billing provider must render once:\n%s", out)
	}
	for _, want := range []string{"OMP 60%", "OpenCode 40%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing harness activity %q:\n%s", want, out)
		}
	}
}

func TestUsagePaneProviderFirstContract_APIAcrossHarnessesIsOneBlock(t *testing.T) {
	blocks := []APIProviderUsage{
		{BackendID: "deepseek", Label: "DeepSeek", Windows: []APIUsageWindow{{WindowMinutes: 1440, Tokens: 600, CostUSD: 0.06}}, PaneActivity: &ProviderPaneActivity{WindowMinutes: 1440, TotalTokens: 600, Panes: []PaneActivityShare{{PaneID: "omp", Label: "OMP", Tokens: 600, SharePercent: 100}}}},
		{BackendID: "deepseek", Label: "DeepSeek", Windows: []APIUsageWindow{{WindowMinutes: 1440, Tokens: 400, CostUSD: 0.04}}, PaneActivity: &ProviderPaneActivity{WindowMinutes: 1440, TotalTokens: 400, Panes: []PaneActivityShare{{PaneID: "opencode", Label: "OpenCode", Tokens: 400, SharePercent: 100}}}},
	}
	merged := MergeAPIProviderUsage(blocks)
	if len(merged) != 1 {
		t.Fatalf("got %d provider blocks, want 1", len(merged))
	}
	out := FormatUsagePanel(nil, merged, 1_800_000_000_000, PanelLayout{Columns: 100, Rows: 40})
	if strings.Count(out, "DeepSeek · API") != 1 {
		t.Fatalf("backend provider must render once:\n%s", out)
	}
	for _, want := range []string{"1.0k", "$0.10", "OMP 60%", "OpenCode 40%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing merged usage %q:\n%s", want, out)
		}
	}
}
