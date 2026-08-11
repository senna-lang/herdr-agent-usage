/**
 * Tests for EstimateStatusMaxColumns.
 */
package core

import "testing"

func TestEstimateStatusMaxColumns(t *testing.T) {
	if EstimateStatusMaxColumns(nil) != nil {
		t.Fatal("expected nil when width unknown")
	}

	w26 := 26
	budget := EstimateStatusMaxColumns(&w26)
	if budget == nil || *budget != 26-SidebarRowOverheadColumns {
		t.Fatalf("got %#v want %d", budget, 26-SidebarRowOverheadColumns)
	}
	got := FormatUsageStatus(usage(310_000, intPtr(1_000_000)), FormatUsageOptions{MaxColumns: budget})
	if got != "⛁ 31% (310k)" {
		t.Fatalf("got %q want full context status", got)
	}

	// At herdr's default sidebar_min_width (18), the row's real usable width
	// is narrower than the raw sidebar width once herdr's own separator and
	// row indent are accounted for. A wide usage string ("100% (1234k)") that
	// would fit an 18-wide budget must not be picked if it can't actually fit
	// the real row, or herdr hard-truncates it with an ellipsis instead of
	// falling back to a shorter complete candidate.
	w18 := 18
	budget = EstimateStatusMaxColumns(&w18)
	if budget == nil || *budget != 18-SidebarRowOverheadColumns {
		t.Fatalf("got %#v want %d", budget, 18-SidebarRowOverheadColumns)
	}
	got = FormatUsageStatus(usage(1_234_000, intPtr(1_234_000)), FormatUsageOptions{MaxColumns: budget})
	if got != "100% (1234k)" {
		t.Fatalf("got %q want narrow-width fallback without icon", got)
	}
}
