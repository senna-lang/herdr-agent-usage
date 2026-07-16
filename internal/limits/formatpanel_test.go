/**
 * Tests for FormatLimitsPanel.
 * Color is disabled in the default layout so assertions use plain text.
 */
package limits

import (
	"strings"
	"testing"
	"unicode/utf8"
)

var wide = PanelLayout{Columns: 60, Rows: 9999, Color: false}

func sampleProvider() ProviderLimits {
	r1, r2 := int64(2_000_000_000), int64(2_000_500_000)
	wm1, wm2 := 300, 10080
	plan := "Plus"
	return ProviderLimits{
		ProviderID:  "codex",
		Label:       "Codex",
		Primary:     &LimitWindow{UsedPercentage: 10, ResetsAt: &r1, WindowMinutes: &wm1},
		Secondary:   &LimitWindow{UsedPercentage: 40, ResetsAt: &r2, WindowMinutes: &wm2},
		PlanType:    &plan,
		Source:      "rollout",
		FetchedAtMs: 1_700_000_000_000,
	}
}

func assertNoANSI(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("unexpected ANSI in %q", text)
	}
}

func TestFormatProviderBlock_HeaderBar(t *testing.T) {
	text := FormatProviderBlock(sampleProvider(), wide, 1_700_000_000_000)
	for _, want := range []string{"Codex", "Plus", "5h", "7d", "90% left", "60% left", "█"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "source:") {
		t.Fatal("should not contain source")
	}
	assertNoANSI(t, text)
}

func TestFormatProviderBlock_EmDash(t *testing.T) {
	text := FormatProviderBlock(ProviderLimits{
		ProviderID: "opencode", Label: "OpenCode", Source: "none", FetchedAtMs: 0,
	}, wide, 0)
	if !strings.Contains(text, "5h") || !strings.Contains(text, "7d") || !strings.Contains(text, "—") {
		t.Fatalf("got:\n%s", text)
	}
	if strings.Contains(text, "note:") {
		t.Fatal("note should not show")
	}
}

func TestFormatProviderBlock_PaneActivity(t *testing.T) {
	p := sampleProvider()
	p.PaneActivity = &ProviderPaneActivity{
		WindowMinutes: 300, TotalTokens: 1000,
		Panes: []PaneActivityShare{
			{PaneID: "w1:p1", Label: "codex-a", Tokens: 700, SharePercent: 70},
			{PaneID: "w1:p2", Label: "codex-b", Tokens: 300, SharePercent: 30},
		},
	}
	text := FormatProviderBlock(p, wide, 1_700_000_000_000)
	for _, want := range []string{"5h share", "codex-a 70%", "codex-b 30%"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestFormatProviderBlock_RunOutWarn(t *testing.T) {
	wm := 300
	plan := "Pro"
	text := FormatProviderBlock(ProviderLimits{
		ProviderID: "claude", Label: "Claude", PlanType: &plan, Source: "cache",
		Primary: &LimitWindow{
			UsedPercentage: 76, WindowMinutes: &wm,
			RunOut: &RunOutEstimate{MinutesToEmpty: 57, EmptyBeforeReset: true},
		},
	}, wide, 0)
	if !strings.Contains(text, "empty in ~") || !strings.Contains(text, "57m") {
		t.Fatalf("got:\n%s", text)
	}
}

func TestFormatProviderBlock_NoRunOutWhenHolds(t *testing.T) {
	wm := 300
	holds := FormatProviderBlock(ProviderLimits{
		ProviderID: "codex", Label: "Codex", Source: "rollout",
		Primary: &LimitWindow{
			UsedPercentage: 20, WindowMinutes: &wm,
			RunOut: &RunOutEstimate{MinutesToEmpty: 600, EmptyBeforeReset: false},
		},
	}, wide, 0)
	if strings.Contains(holds, "empty in") {
		t.Fatal("should not warn")
	}
	if strings.Contains(FormatProviderBlock(sampleProvider(), wide, 0), "empty in") {
		t.Fatal("sample should not warn")
	}
}

func TestFormatProviderBlock_Color(t *testing.T) {
	text := FormatProviderBlock(sampleProvider(), PanelLayout{Columns: 60, Rows: 9999, Color: true}, 0)
	if !strings.Contains(text, "\x1b[") {
		t.Fatal("expected ANSI")
	}
}

func TestFormatLimitsPanel_Hints(t *testing.T) {
	text := FormatLimitsPanel([]ProviderLimits{sampleProvider()}, 1_700_000_000_000, wide)
	if strings.Contains(text, "Agent Usage") {
		t.Fatal("should not duplicate pane label")
	}
	if !strings.Contains(text, "q quit") || !strings.Contains(text, "Codex") {
		t.Fatalf("got:\n%s", text)
	}
}

func TestFormatLimitsPanel_Empty(t *testing.T) {
	text := FormatLimitsPanel(nil, 1_700_000_000_000, wide)
	if !strings.Contains(text, "no usage data") {
		t.Fatalf("got:\n%s", text)
	}
}

func TestFormatLimitsPanel_CompactTier(t *testing.T) {
	three := []ProviderLimits{
		func() ProviderLimits { p := sampleProvider(); p.Label = "A"; return p }(),
		func() ProviderLimits { p := sampleProvider(); p.Label = "B"; return p }(),
		func() ProviderLimits { p := sampleProvider(); p.Label = "C"; return p }(),
	}
	rich := FormatLimitsPanel(three, 1_700_000_000_000, PanelLayout{Columns: 60, Rows: 9999, Color: false})
	compact := FormatLimitsPanel(three, 1_700_000_000_000, PanelLayout{Columns: 60, Rows: 9, Color: false})
	if len(strings.Split(compact, "\n")) >= len(strings.Split(rich, "\n")) {
		t.Fatal("compact should be shorter")
	}
	for _, name := range []string{"A", "B", "C", "90%"} {
		if !strings.Contains(compact, name) {
			t.Fatalf("missing %q", name)
		}
	}
}

func TestFormatLimitsPanel_CompactWarn(t *testing.T) {
	wm := 300
	warned := sampleProvider()
	warned.Primary = &LimitWindow{
		UsedPercentage: 90, WindowMinutes: &wm,
		RunOut: &RunOutEstimate{MinutesToEmpty: 20, EmptyBeforeReset: true},
	}
	compact := FormatLimitsPanel([]ProviderLimits{warned, warned, warned}, 1_700_000_000_000, PanelLayout{Columns: 60, Rows: 9, Color: false})
	if !strings.Contains(compact, "⚠") {
		t.Fatalf("got:\n%s", compact)
	}
}

func TestFormatLimitsPanel_NoSoftWrap(t *testing.T) {
	wm1, wm2 := 300, 10080
	r1, r2 := int64(2_000_000_000), int64(2_000_500_000)
	plan := "Pro"
	rich := ProviderLimits{
		ProviderID: "claude", Label: "Claude", PlanType: &plan, Source: "cache",
		Primary: &LimitWindow{
			UsedPercentage: 82, WindowMinutes: &wm1, ResetsAt: &r1,
			RunOut: &RunOutEstimate{MinutesToEmpty: 23, EmptyBeforeReset: true},
		},
		Secondary: &LimitWindow{
			UsedPercentage: 71, WindowMinutes: &wm2, ResetsAt: &r2,
			RunOut: &RunOutEstimate{MinutesToEmpty: 4080, EmptyBeforeReset: true},
		},
		PaneActivity: &ProviderPaneActivity{
			WindowMinutes: 300, TotalTokens: 100,
			Panes: []PaneActivityShare{
				{PaneID: "a", Label: "claude", Tokens: 81, SharePercent: 81.5},
				{PaneID: "b", Label: "claude", Tokens: 10, SharePercent: 10.5},
				{PaneID: "__other__", Label: "closed / other", Tokens: 8, SharePercent: 8},
			},
		},
	}
	for _, columns := range []int{20, 24, 30, 32, 38, 40, 44, 50, 60, 80} {
		for _, rows := range []int{8, 16, 40} {
			text := FormatLimitsPanel([]ProviderLimits{rich, rich, rich}, 1_700_000_000_000, PanelLayout{Columns: columns, Rows: rows, Color: false})
			for _, line := range strings.Split(text, "\n") {
				if utf8.RuneCountInString(line) > columns {
					t.Fatalf("line wider than %d: %q (%d)", columns, line, utf8.RuneCountInString(line))
				}
			}
		}
	}
}

func TestFormatLimitsPanel_RowBudget(t *testing.T) {
	many := make([]ProviderLimits, 4)
	for i := range many {
		p := sampleProvider()
		p.Label = "P" + string(rune('0'+i))
		many[i] = p
	}
	text := FormatLimitsPanel(many, 1_700_000_000_000, PanelLayout{Columns: 40, Rows: 15, Color: false})
	if len(strings.Split(text, "\n")) > 16 {
		t.Fatalf("too many lines: %d", len(strings.Split(text, "\n")))
	}
}
