/**
 * Tests for bar rendering and ANSI helpers.
 */
package bar

import (
	"math"
	"testing"
	"unicode/utf8"
)

func TestRenderBar_AlwaysMatchesWidth(t *testing.T) {
	for _, r := range []float64{0, 5, 27, 50, 90, 100} {
		for _, w := range []int{1, 5, 8, 12, 18, 24} {
			bar := RenderBar(r, w)
			if utf8.RuneCountInString(bar) != w {
				t.Fatalf("remaining=%v width=%d len=%d bar=%q", r, w, utf8.RuneCountInString(bar), bar)
			}
		}
	}
}

func TestRenderBar_0And100(t *testing.T) {
	if got := RenderBar(0, 6); got != "░░░░░░" {
		t.Fatalf("0%% = %q", got)
	}
	if got := RenderBar(100, 6); got != "██████" {
		t.Fatalf("100%% = %q", got)
	}
}

func TestRenderBar_50RoughlyHalf(t *testing.T) {
	bar := RenderBar(50, 10)
	filled := 0
	for _, r := range bar {
		if r == '█' {
			filled++
		}
	}
	if filled < 4 || filled > 6 {
		t.Fatalf("filled=%d bar=%q", filled, bar)
	}
}

func TestRenderBar_OutOfRangePreservesWidth(t *testing.T) {
	for _, r := range []float64{-20, 300, 0} {
		// NaN covered separately via math.NaN if needed; -20 and 300 here
		if utf8.RuneCountInString(RenderBar(r, 8)) != 8 {
			t.Fatalf("remaining=%v width mismatch", r)
		}
	}
	// explicit NaN
	if utf8.RuneCountInString(RenderBar(math.NaN(), 8)) != 8 {
		t.Fatal("NaN width mismatch")
	}
}

func TestToneForRemaining(t *testing.T) {
	cases := []struct {
		rem  float64
		want BarTone
	}{
		{80, ToneHigh},
		{50, ToneHigh},
		{49, ToneMid},
		{20, ToneMid},
		{19, ToneLow},
		{0, ToneLow},
	}
	for _, tc := range cases {
		if got := ToneForRemaining(tc.rem); got != tc.want {
			t.Fatalf("ToneForRemaining(%v)=%q want %q", tc.rem, got, tc.want)
		}
	}
}

func TestColorizeDimBold_Disabled(t *testing.T) {
	if Colorize("x", ToneHigh, false) != "x" {
		t.Fatal("colorize pass-through")
	}
	if Dim("x", false) != "x" {
		t.Fatal("dim pass-through")
	}
	if Bold("x", false) != "x" {
		t.Fatal("bold pass-through")
	}
}

func TestColorizeDimBold_Enabled(t *testing.T) {
	if got := Colorize("x", ToneLow, true); got != "\x1b[31mx\x1b[0m" {
		t.Fatalf("colorize = %q", got)
	}
	if got := Dim("x", true); got != "\x1b[2mx\x1b[0m" {
		t.Fatalf("dim = %q", got)
	}
	if got := Bold("x", true); got != "\x1b[1mx\x1b[0m" {
		t.Fatalf("bold = %q", got)
	}
}
