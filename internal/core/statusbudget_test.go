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
	if budget == nil || *budget != 26 {
		t.Fatalf("got %#v want 26", budget)
	}
	got := FormatUsageStatus(usage(310_000, intPtr(1_000_000)), FormatUsageOptions{MaxColumns: budget})
	if got != "⛁ 31% (310k)" {
		t.Fatalf("got %q want full context status", got)
	}
}
