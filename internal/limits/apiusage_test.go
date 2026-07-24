/**
 * Tests for pay-as-you-go backend aggregation and rendering.
 */
package limits

import (
	"strings"
	"testing"
)

const apiNowMs int64 = 1_800_000_000_000

func minutesAgo(mins int) int64 {
	return apiNowMs - int64(mins)*60_000
}

func apiRow(createdMs int64, providerID, modelID string, tokens, cost float64) OpenCodeTokenRow {
	return OpenCodeTokenRow{
		TimeCreated: createdMs,
		Data: mustJSON(map[string]any{
			"role": "assistant", "providerID": providerID, "modelID": modelID,
			"cost":   cost,
			"tokens": map[string]any{"input": tokens},
		}),
	}
}

func TestDecodeAPIUsageRows_FiltersBackendAndRole(t *testing.T) {
	rows := []OpenCodeTokenRow{
		apiRow(minutesAgo(10), "deepseek", "deepseek-chat", 100, 0.01),
		apiRow(minutesAgo(20), "ollama", "qwen", 500, 0),
		{TimeCreated: minutesAgo(5), Data: mustJSON(map[string]any{"role": "user", "providerID": "deepseek"})},
		{TimeCreated: minutesAgo(5), Data: "not json"},
	}
	got := DecodeAPIUsageRows(rows, "deepseek")
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1: %#v", len(got), got)
	}
	if got[0].Tokens != 100 || got[0].CostUSD != 0.01 || got[0].ModelID != "deepseek-chat" {
		t.Fatalf("unexpected row: %#v", got[0])
	}
}

func TestDecodeAPIUsageRows_SumsAllTokenKinds(t *testing.T) {
	rows := []OpenCodeTokenRow{{
		TimeCreated: minutesAgo(1),
		Data: mustJSON(map[string]any{
			"role": "assistant", "providerID": "deepseek", "modelID": "m",
			"tokens": map[string]any{
				"input": 10, "output": 5, "reasoning": 4,
				"cache": map[string]any{"read": 20, "write": 1},
			},
		}),
	}}
	got := DecodeAPIUsageRows(rows, "deepseek")
	if len(got) != 1 || got[0].Tokens != 40 {
		t.Fatalf("got %#v, want tokens=40", got)
	}
}

func TestDecodeAPIUsageRows_PrefersBodyTimestamp(t *testing.T) {
	body := float64(minutesAgo(30))
	rows := []OpenCodeTokenRow{{
		TimeCreated: minutesAgo(9000),
		Data: mustJSON(map[string]any{
			"role": "assistant", "providerID": "deepseek", "modelID": "m",
			"tokens": map[string]any{"input": 1},
			"time":   map[string]any{"created": body},
		}),
	}}
	got := DecodeAPIUsageRows(rows, "deepseek")
	if len(got) != 1 || got[0].CreatedMs != int64(body) {
		t.Fatalf("got %#v, want created=%v", got, int64(body))
	}
}

func TestSumAPIWindows_NestedWindows(t *testing.T) {
	rows := DecodeAPIUsageRows([]OpenCodeTokenRow{
		apiRow(minutesAgo(30), "deepseek", "a", 100, 0.01),        // in 24h/7d/30d
		apiRow(minutesAgo(3*24*60), "deepseek", "a", 200, 0.02),   // in 7d/30d
		apiRow(minutesAgo(20*24*60), "deepseek", "a", 400, 0.04),  // in 30d only
		apiRow(minutesAgo(400*24*60), "deepseek", "a", 800, 0.08), // outside all
	}, "deepseek")

	got := SumAPIWindows(rows, apiNowMs, APIUsageWindowMinutes)
	if len(got) != 3 {
		t.Fatalf("got %d windows", len(got))
	}
	if got[0].Tokens != 100 || got[0].CostUSD != 0.01 {
		t.Fatalf("24h: %#v", got[0])
	}
	if got[1].Tokens != 300 {
		t.Fatalf("7d tokens: %v want 300", got[1].Tokens)
	}
	if got[2].Tokens != 700 {
		t.Fatalf("30d tokens: %v want 700", got[2].Tokens)
	}
}

func TestSumAPIModels_RichestFirst(t *testing.T) {
	rows := DecodeAPIUsageRows([]OpenCodeTokenRow{
		apiRow(minutesAgo(10), "deepseek", "flash", 73529, 0.0031),
		apiRow(minutesAgo(10), "deepseek", "pro", 224301, 0.016),
		apiRow(minutesAgo(10), "deepseek", "chat", 33391, 0.0047),
		// Outside the window: must not appear.
		apiRow(minutesAgo(5*24*60), "deepseek", "old", 999, 9.99),
	}, "deepseek")

	got := SumAPIModels(rows, apiNowMs, APIShareWindowMinutes)
	if len(got) != 3 {
		t.Fatalf("got %d models: %#v", len(got), got)
	}
	want := []string{"pro", "chat", "flash"}
	for i, w := range want {
		if got[i].ModelID != w {
			t.Fatalf("position %d: got %q want %q", i, got[i].ModelID, w)
		}
	}
}

func TestSumAPIModels_GroupsRepeatedModel(t *testing.T) {
	rows := DecodeAPIUsageRows([]OpenCodeTokenRow{
		apiRow(minutesAgo(10), "deepseek", "pro", 100, 0.01),
		apiRow(minutesAgo(20), "deepseek", "pro", 200, 0.02),
	}, "deepseek")
	got := SumAPIModels(rows, apiNowMs, APIShareWindowMinutes)
	if len(got) != 1 || got[0].Tokens != 300 || got[0].CostUSD != 0.03 {
		t.Fatalf("got %#v", got)
	}
}

func TestAnyAPICost(t *testing.T) {
	if AnyAPICost([]APIUsageWindow{{CostUSD: 0}, {CostUSD: 0}}) {
		t.Fatal("all-zero cost should report false")
	}
	if !AnyAPICost([]APIUsageWindow{{CostUSD: 0}, {CostUSD: 0.01}}) {
		t.Fatal("nonzero cost should report true")
	}
}

func TestMergeAPIProviderUsage_GroupsSameBackendAcrossHarnesses(t *testing.T) {
	blocks := []APIProviderUsage{
		{BackendID: "deepseek", Label: "DeepSeek", Windows: []APIUsageWindow{{WindowMinutes: 1440, Tokens: 100, CostUSD: .01}}, Models: []APIModelUsage{{ModelID: "chat", Tokens: 100, CostUSD: .01}}, PaneActivity: &ProviderPaneActivity{WindowMinutes: 1440, TotalTokens: 100, Panes: []PaneActivityShare{{PaneID: "open", Label: "OpenCode", Tokens: 100}}}},
		{BackendID: "deepseek", Label: "DeepSeek", Windows: []APIUsageWindow{{WindowMinutes: 1440, Tokens: 50, CostUSD: .02}}, Models: []APIModelUsage{{ModelID: "chat", Tokens: 20, CostUSD: .01}, {ModelID: "reasoner", Tokens: 30, CostUSD: .01}}, PaneActivity: &ProviderPaneActivity{WindowMinutes: 1440, TotalTokens: 50, Panes: []PaneActivityShare{{PaneID: "omp", Label: "OMP", Tokens: 50}}}},
	}
	got := MergeAPIProviderUsage(blocks)
	if len(got) != 1 {
		t.Fatalf("got %d blocks: %#v", len(got), got)
	}
	block := got[0]
	if block.Windows[0].Tokens != 150 || block.Windows[0].CostUSD != .03 || len(block.Models) != 2 {
		t.Fatalf("merged block=%#v", block)
	}
	if block.PaneActivity == nil || len(block.PaneActivity.Panes) != 2 || block.PaneActivity.TotalTokens != 150 {
		t.Fatalf("activity=%#v", block.PaneActivity)
	}
}

func TestHumanizeBackendID(t *testing.T) {
	cases := map[string]string{
		"deepseek":    "Deepseek",
		"my-deepseek": "My Deepseek",
		"z_ai":        "Z Ai",
		"":            "",
	}
	for in, want := range cases {
		if got := HumanizeBackendID(in); got != want {
			t.Fatalf("HumanizeBackendID(%q)=%q want %q", in, got, want)
		}
	}
}

func sampleAPIUsage() APIProviderUsage {
	return APIProviderUsage{
		BackendID: "deepseek",
		Label:     "DeepSeek",
		Windows: []APIUsageWindow{
			{WindowMinutes: 24 * 60, Tokens: 331683, CostUSD: 0.0238},
			{WindowMinutes: 7 * 24 * 60, Tokens: 331683, CostUSD: 0.0238},
			{WindowMinutes: 30 * 24 * 60, Tokens: 331683, CostUSD: 0.0238},
		},
		Models: []APIModelUsage{
			{ModelID: "deepseek-v4-pro", Tokens: 224301, CostUSD: 0.016},
			{ModelID: "deepseek-chat", Tokens: 33391, CostUSD: 0.0047},
		},
		PaneActivity: &ProviderPaneActivity{
			WindowMinutes: 24 * 60,
			TotalTokens:   331683,
			Panes:         []PaneActivityShare{{PaneID: "w6:p2K", Label: "term", SharePercent: 100}},
		},
		HasCost: true,
	}
}

func TestAPIRichBlock_RendersHeaderWindowsModelsShare(t *testing.T) {
	layout := PanelLayout{Columns: 44, Rows: 40}
	lines := apiRichBlock(sampleAPIUsage(), layout, true)

	if len(lines) != 6 {
		t.Fatalf("got %d lines: %#v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "DeepSeek") || !strings.Contains(lines[0], "API") {
		t.Fatalf("header: %q", lines[0])
	}
	for i, tag := range []string{"24h", "7d", "30d"} {
		if !strings.Contains(lines[1+i], tag) {
			t.Fatalf("window %d: %q missing %q", i, lines[1+i], tag)
		}
	}
	// Tokens are the base column; USD trails it only when priced.
	if !strings.Contains(lines[1], "332k") || !strings.Contains(lines[1], "$0.02") {
		t.Fatalf("24h row: %q", lines[1])
	}
	if strings.Index(lines[1], "332k") > strings.Index(lines[1], "$0.02") {
		t.Fatalf("tokens must precede cost: %q", lines[1])
	}
	if !strings.Contains(lines[4], "models") || !strings.Contains(lines[4], "deepseek-v4-pro") {
		t.Fatalf("models row: %q", lines[4])
	}
	// Model breakdown is token-based, so cost rounding cannot collapse
	// distinct models onto the same "$0.02".
	if strings.Contains(lines[4], "$") {
		t.Fatalf("models row must be token-based: %q", lines[4])
	}
	if !strings.Contains(lines[5], "term") {
		t.Fatalf("share row: %q", lines[5])
	}
}

func TestAPIRichBlock_OmitsCostWhenUnpriced(t *testing.T) {
	usage := sampleAPIUsage()
	usage.HasCost = false
	lines := apiRichBlock(usage, PanelLayout{Columns: 44, Rows: 40}, true)
	for _, line := range lines {
		if strings.Contains(line, "$") {
			t.Fatalf("unpriced backend must not render a cost column: %q", line)
		}
	}
	if !strings.Contains(lines[1], "332k") {
		t.Fatalf("tokens must still render: %q", lines[1])
	}
}

func TestAPIModelsLine_FitsMoreAsWidthGrows(t *testing.T) {
	models := []APIModelUsage{
		{ModelID: "deepseek-v4-pro", Tokens: 224301},
		{ModelID: "deepseek-chat", Tokens: 33391},
		{ModelID: "deepseek-v4-flash", Tokens: 73529},
	}
	narrow := apiModelsLine(models, PanelLayout{Columns: 44})
	if !strings.Contains(narrow, "+2") {
		t.Fatalf("44 columns should elide two models: %q", narrow)
	}
	wide := apiModelsLine(models, PanelLayout{Columns: 100})
	if strings.Contains(wide, "+") {
		t.Fatalf("100 columns should fit every model: %q", wide)
	}
	for _, m := range models {
		if !strings.Contains(wide, m.ModelID) {
			t.Fatalf("wide row missing %q: %q", m.ModelID, wide)
		}
	}
}

func TestAPIRichBlock_SlimDropsExtras(t *testing.T) {
	lines := apiRichBlock(sampleAPIUsage(), PanelLayout{Columns: 44, Rows: 40}, false)
	if len(lines) != 4 {
		t.Fatalf("slim tier should be header + 3 windows, got %#v", lines)
	}
}

func TestFormatUsagePanel_RendersBothKinds(t *testing.T) {
	five := 300
	sub := ProviderLimits{
		ProviderID: "opencode", Label: "OpenCode", PlanType: strPtr("Go"),
		Primary: &LimitWindow{UsedPercentage: 0, WindowMinutes: &five},
	}
	out := FormatUsagePanel([]ProviderLimits{sub}, []APIProviderUsage{sampleAPIUsage()}, apiNowMs, PanelLayout{Columns: 44, Rows: 40})
	if !strings.Contains(out, "OpenCode") {
		t.Fatal("subscription block missing")
	}
	if !strings.Contains(out, "DeepSeek") {
		t.Fatal("api block missing")
	}
	if strings.Index(out, "OpenCode") > strings.Index(out, "DeepSeek") {
		t.Fatal("subscription blocks should render before api blocks")
	}
}

func TestFormatUsagePanel_APIOnlyIsNotEmpty(t *testing.T) {
	out := FormatUsagePanel(nil, []APIProviderUsage{sampleAPIUsage()}, apiNowMs, PanelLayout{Columns: 44, Rows: 40})
	if strings.Contains(out, "no usage data yet") {
		t.Fatalf("api-only panel must not render the empty state:\n%s", out)
	}
	if !strings.Contains(out, "DeepSeek") {
		t.Fatalf("api block missing:\n%s", out)
	}
}

func TestFormatUsagePanel_ShortPaneDegradesBothKinds(t *testing.T) {
	five := 300
	sub := ProviderLimits{
		ProviderID: "opencode", Label: "OpenCode", PlanType: strPtr("Go"),
		Primary: &LimitWindow{UsedPercentage: 0, WindowMinutes: &five},
	}
	// Rows budget far below the rich tiers forces the compact tier.
	out := FormatUsagePanel([]ProviderLimits{sub}, []APIProviderUsage{sampleAPIUsage()}, apiNowMs, PanelLayout{Columns: 44, Rows: 8})
	if strings.Contains(out, "models") {
		t.Fatalf("compact tier must drop the models row:\n%s", out)
	}
	if !strings.Contains(out, "DeepSeek") || !strings.Contains(out, "OpenCode") {
		t.Fatalf("both blocks must survive compaction:\n%s", out)
	}
}

func TestSortAPIProviderUsage_RichestFirst(t *testing.T) {
	cheap := APIProviderUsage{BackendID: "a", Windows: []APIUsageWindow{{CostUSD: 0.01}}}
	rich := APIProviderUsage{BackendID: "b", Windows: []APIUsageWindow{{CostUSD: 1.82}}}
	blocks := []APIProviderUsage{cheap, rich}
	sortAPIProviderUsage(blocks)
	if blocks[0].BackendID != "b" {
		t.Fatalf("got %#v", blocks)
	}
}
