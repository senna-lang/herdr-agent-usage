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

func TestFormatCompactContextTokens(t *testing.T) {
	if got := FormatCompactContextTokens(usage(94_000, intPtr(200_000))); got != "⛁ 94k" {
		t.Fatalf("got %q want ⛁ 94k", got)
	}
	if got := FormatCompactContextTokens(usage(5_000, nil)); got != "⛁ 5.0k" {
		t.Fatalf("got %q want ⛁ 5.0k", got)
	}
	if got := FormatCompactContextTokens(usage(0, intPtr(200_000))); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestContextTierTokenName(t *testing.T) {
	cases := []struct {
		tokens int
		want   string
	}{
		{0, ""},
		{99_999, ContextTokenOK},
		{100_000, ContextTokenSoft},
		{149_999, ContextTokenSoft},
		{150_000, ContextTokenWarn},
		{179_999, ContextTokenWarn},
		{180_000, ContextTokenCritical},
		{250_000, ContextTokenCritical},
	}
	for _, tc := range cases {
		if got := ContextTierTokenName(tc.tokens); got != tc.want {
			t.Fatalf("ContextTierTokenName(%d)=%q want %q", tc.tokens, got, tc.want)
		}
	}
}

func TestContextTierTokenValues_Exclusive(t *testing.T) {
	got := ContextTierTokenValues(usage(185_000, nil))
	if got[ContextTokenCritical] != "⛁ 185k" {
		t.Fatalf("crit=%q", got[ContextTokenCritical])
	}
	if got[ContextTokenOK] != "" || got[ContextTokenSoft] != "" || got[ContextTokenWarn] != "" {
		t.Fatalf("non-exclusive: %#v", got)
	}
	empty := ContextTierTokenValues(usage(0, nil))
	for _, name := range AllContextTierTokenNames {
		if empty[name] != "" {
			t.Fatalf("%s should be empty", name)
		}
	}
}

func TestFormatUsageStatus_Exactly1000OneDecimal(t *testing.T) {
	got := FormatUsageStatus(usage(1000, intPtr(1_000_000)), FormatUsageOptions{})
	if got != "⛁ 0% (1.0k)" {
		t.Fatalf("got %q", got)
	}
}

func TestUsageStatusCandidates_Compacted(t *testing.T) {
	u := usage(13_820, intPtr(200_000))
	u.Compacted = true
	got := UsageStatusCandidates(u)
	want := []string{"⛁ compacted (14k)", "compacted (14k)", "compacted", "14k"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFormatUsageStatus_CompactedLabel(t *testing.T) {
	u := usage(13_820, intPtr(200_000))
	u.Compacted = true
	if got := FormatUsageStatus(u, FormatUsageOptions{}); got != "⛁ compacted (14k)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_CompactedDegrades(t *testing.T) {
	u := usage(13_820, intPtr(200_000))
	u.Compacted = true
	max := 9
	if got := FormatUsageStatus(u, FormatUsageOptions{MaxColumns: &max}); got != "compacted" {
		t.Fatalf("got %q", got)
	}
	max = 4
	if got := FormatUsageStatus(u, FormatUsageOptions{MaxColumns: &max}); got != "14k" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUsageStatus_CompactedWithoutWindow(t *testing.T) {
	if got := FormatUsageStatus(ContextUsage{ContextTokens: 13_820, Compacted: true}, FormatUsageOptions{}); got != "⛁ compacted (14k)" {
		t.Fatalf("got %q", got)
	}
}
