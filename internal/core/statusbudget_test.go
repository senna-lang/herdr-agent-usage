/**
 * Tests for EstimateStatusMaxColumns.
 */
package core

import "testing"

func TestEstimateStatusMaxColumns(t *testing.T) {
	if EstimateStatusMaxColumns(nil, strP("claude")) != nil {
		t.Fatal("expected nil when width unknown")
	}
	w32 := 32
	label := "claude"
	budget := EstimateStatusMaxColumns(&w32, &label)
	if budget == nil || *budget != 32-SidebarRowOverheadColumns-6 {
		t.Fatalf("got %#v", budget)
	}
	if *budget < 13 {
		t.Fatal("should leave room for full form")
	}

	w26 := 26
	budget = EstimateStatusMaxColumns(&w26, &label)
	if budget == nil || *budget != 8 {
		t.Fatalf("got %#v want 8", budget)
	}

	w21 := 21
	budget = EstimateStatusMaxColumns(&w21, &label)
	if budget == nil || *budget != 3 {
		t.Fatalf("got %#v want 3", budget)
	}

	w18 := 18
	op := "opencode"
	budget = EstimateStatusMaxColumns(&w18, &op)
	if budget == nil || *budget != 3 {
		t.Fatalf("floor got %#v", budget)
	}

	budget = EstimateStatusMaxColumns(&w26, nil)
	if budget == nil || *budget != 26-SidebarRowOverheadColumns {
		t.Fatalf("nil label got %#v", budget)
	}
}

func strP(s string) *string { return &s }
