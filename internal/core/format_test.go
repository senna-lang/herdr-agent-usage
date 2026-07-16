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
