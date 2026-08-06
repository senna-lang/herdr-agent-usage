/**
 * Tests for formatUsageStatus / displayWidth.
 */
package core

import (
	"testing"
)

func usage(contextTokens int, windowTokens *int) ContextUsage {
	return ContextUsage{ContextTokens: contextTokens, WindowTokens: windowTokens}
}

func intPtr(v int) *int { return &v }

func TestDisplayWidth_ASCII(t *testing.T) {
	if got := DisplayWidth("31% (310k)"); got != 10 {
		t.Fatalf("DisplayWidth = %d, want 10", got)
	}
}

func TestDisplayWidth_DiskIcon(t *testing.T) {
	// ⛁(2) + space(1) + "31% (310k)"(10) = 13
	if got := DisplayWidth("⛁ 31% (310k)"); got != 13 {
		t.Fatalf("DisplayWidth = %d, want 13", got)
	}
}

func TestDisplayWidth_WarningIcon(t *testing.T) {
	// ⚠️(2) + space(1) + "80% (160k)"(10) = 13
	if got := DisplayWidth("⚠️ 80% (160k)"); got != 13 {
		t.Fatalf("DisplayWidth = %d, want 13", got)
	}
}

func TestUsageStatusCandidates_WithWindow(t *testing.T) {
	got := UsageStatusCandidates(usage(310_000, intPtr(1_000_000)))
	want := []string{"⛁ 31% (310k)", "31% (310k)", "31%"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestUsageStatusCandidates_WithoutWindow(t *testing.T) {
	got := UsageStatusCandidates(usage(5_000, nil))
	want := []string{"⛁ 5.0k", "5.0k"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFormatUsageStatus_FullWhenNoMax(t *testing.T) {
	got := FormatUsageStatus(usage(310_000, intPtr(1_000_000)), FormatUsageOptions{})
	if got != "⛁ 31% (310k)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_DropsIconWhenNarrow(t *testing.T) {
	max := 12
	got := FormatUsageStatus(usage(310_000, intPtr(1_000_000)), FormatUsageOptions{MaxColumns: &max})
	if got != "31% (310k)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_PercentOnlyWhenNarrower(t *testing.T) {
	max := 5
	got := FormatUsageStatus(usage(310_000, intPtr(1_000_000)), FormatUsageOptions{MaxColumns: &max})
	if got != "31%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_KeepsFullWhenRoom(t *testing.T) {
	max := 20
	got := FormatUsageStatus(usage(310_000, intPtr(1_000_000)), FormatUsageOptions{MaxColumns: &max})
	if got != "⛁ 31% (310k)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_NoWindowAbsolute(t *testing.T) {
	got := FormatUsageStatus(usage(5_000, nil), FormatUsageOptions{})
	if got != "⛁ 5.0k" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_NoWindowNarrow(t *testing.T) {
	max := 5
	got := FormatUsageStatus(usage(5_000, nil), FormatUsageOptions{MaxColumns: &max})
	if got != "5.0k" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_ClampsOver100WithWarning(t *testing.T) {
	got := FormatUsageStatus(usage(999_999, intPtr(200_000)), FormatUsageOptions{})
	if got != "⚠️ 100% (1000k)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_Exactly80Warning(t *testing.T) {
	got := FormatUsageStatus(usage(160_000, intPtr(200_000)), FormatUsageOptions{})
	if got != "⚠️ 80% (160k)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_79NormalIcon(t *testing.T) {
	got := FormatUsageStatus(usage(158_000, intPtr(200_000)), FormatUsageOptions{})
	if got != "⛁ 79% (158k)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_NoWindowNoWarning(t *testing.T) {
	got := FormatUsageStatus(usage(999_999, nil), FormatUsageOptions{})
	if got != "⛁ 1000k" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_Below1000NoK(t *testing.T) {
	got := FormatUsageStatus(usage(543, intPtr(200_000)), FormatUsageOptions{})
	if got != "⛁ 0% (543)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_Exactly1000OneDecimal(t *testing.T) {
	got := FormatUsageStatus(usage(1000, intPtr(1_000_000)), FormatUsageOptions{})
	if got != "⛁ 0% (1.0k)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_FractionFull(t *testing.T) {
	got := FormatUsageStatus(usage(33_000, intPtr(200_000)), FormatUsageOptions{Fraction: true})
	if got != "⛁ 33k/200k" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_FractionDegradesToCountAlone(t *testing.T) {
	max := 5
	got := FormatUsageStatus(usage(33_000, intPtr(200_000)), FormatUsageOptions{MaxColumns: &max, Fraction: true})
	if got != "33k" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_FractionKeepsWarningIcon(t *testing.T) {
	got := FormatUsageStatus(usage(160_000, intPtr(200_000)), FormatUsageOptions{Fraction: true})
	if got != "⚠️ 160k/200k" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_FractionMillionWindow(t *testing.T) {
	got := FormatUsageStatus(usage(33_000, intPtr(1_000_000)), FormatUsageOptions{Fraction: true})
	if got != "⛁ 33k/1.0m" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_FractionNoWindowAbsolute(t *testing.T) {
	got := FormatUsageStatus(usage(5_000, nil), FormatUsageOptions{Fraction: true})
	if got != "⛁ 5.0k" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_GaugeStyle(t *testing.T) {
	window := 200_000
	cases := map[int]string{
		12_000:  "▁ 12k/200k",
		55_000:  "▂ 55k/200k",
		102_000: "▄ 102k/200k",
		130_000: "▆ 130k/200k",
		180_000: "█ 180k/200k",
	}
	for tokens, want := range cases {
		usage := ContextUsage{ContextTokens: tokens, WindowTokens: &window}
		got := FormatUsageStatus(usage, FormatUsageOptions{Fraction: true, IconStyle: "gauge"})
		if got != want {
			t.Fatalf("tokens=%d got %q want %q", tokens, got, want)
		}
	}
}

func TestFormatUsageStatus_GaugeUnknownWindowHasNoIcon(t *testing.T) {
	got := FormatUsageStatus(ContextUsage{ContextTokens: 97_000}, FormatUsageOptions{Fraction: true, IconStyle: "gauge"})
	if got != "97k" {
		t.Fatalf("got %q want %q", got, "97k")
	}
}

func TestFormatUsageStatus_NoneStyle(t *testing.T) {
	window := 200_000
	usage := ContextUsage{ContextTokens: 102_000, WindowTokens: &window}
	got := FormatUsageStatus(usage, FormatUsageOptions{Fraction: true, IconStyle: "none"})
	if got != "102k/200k" {
		t.Fatalf("got %q want %q", got, "102k/200k")
	}
}

func TestPadStatusRight(t *testing.T) {
	// "claude"(6) + " · "(3) + "▄ 95k/200k"(10) = 19; row 30 → 11 pad cells
	got := PadStatusRight("▄ 95k/200k", 6, 30)
	if got != "⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀▄ 95k/200k" {
		t.Fatalf("got %q", got)
	}
	if DisplayWidth(got) != 30-6-3 {
		t.Fatalf("padded width=%d, want %d", DisplayWidth(got), 30-6-3)
	}
}

func TestPadStatusRight_NoRoomLeavesUnchanged(t *testing.T) {
	if got := PadStatusRight("▄ 95k/200k", 20, 30); got != "▄ 95k/200k" {
		t.Fatalf("got %q", got)
	}
	if got := PadStatusRight("▄ 95k/200k", 17, 30); got != "▄ 95k/200k" {
		t.Fatalf("exact fit: got %q", got)
	}
}

func TestContextLevelFor(t *testing.T) {
	if got := ContextLevelFor(nil); got != "" {
		t.Fatalf("nil percent: got %q", got)
	}
	cases := map[int]string{0: "", 59: "", 60: "warm", 84: "warm", 85: "hot", 100: "hot"}
	for percent, want := range cases {
		p := percent
		if got := ContextLevelFor(&p); got != want {
			t.Fatalf("percent=%d got %q want %q", percent, got, want)
		}
	}
}
