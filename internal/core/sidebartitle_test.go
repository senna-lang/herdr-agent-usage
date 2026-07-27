/**
 * Tests for ResolveSidebarTitle's tab/space/pane fallback composition.
 */
package core

import "testing"

func TestResolveSidebarTitle_NamedTabNoPane(t *testing.T) {
	got := ResolveSidebarTitle("", "Task A", 1, "herdr-agent-usage")
	if got != "Task A" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSidebarTitle_DefaultNumericTabFallsBackToSpace(t *testing.T) {
	// Herdr defaults a fresh tab's label to its own number ("1").
	got := ResolveSidebarTitle("", "1", 1, "logosyncs")
	if got != "logosyncs" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSidebarTitle_EmptyTabFallsBackToSpace(t *testing.T) {
	got := ResolveSidebarTitle("", "", 1, "logosyncs")
	if got != "logosyncs" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSidebarTitle_DefaultTabWithPaneJoinsSpaceAndPane(t *testing.T) {
	got := ResolveSidebarTitle("worker", "1", 1, "logosyncs")
	if got != "logosyncs・worker" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSidebarTitle_NamedTabWithPaneJoinsTabAndPane(t *testing.T) {
	got := ResolveSidebarTitle("worker", "Task A", 1, "herdr-agent-usage")
	if got != "Task A・worker" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSidebarTitle_PaneOnlyWhenBaseEmpty(t *testing.T) {
	got := ResolveSidebarTitle("worker", "", 1, "")
	if got != "worker" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSidebarTitle_NumberBoundaryNotMistakenForDefault(t *testing.T) {
	// A tab genuinely renamed to the literal string "2" while it happens to
	// be tab number 1 must NOT be treated as unset (edge case: only the
	// matching number counts as the auto-assigned default).
	got := ResolveSidebarTitle("", "2", 1, "space")
	if got != "2" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSidebarTitle_AllEmpty(t *testing.T) {
	got := ResolveSidebarTitle("", "", 0, "")
	if got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestTabLabelIsDefault(t *testing.T) {
	for _, tc := range []struct {
		label  string
		number int
		want   bool
	}{
		{"", 1, true},        // no label at all
		{"1", 1, true},       // Herdr's auto-assigned label for tab 1
		{"12", 12, true},     // two-digit tab number
		{"2", 1, false},      // renamed to a number that is not its own
		{"Task A", 1, false}, // ordinary rename
		{"01", 1, false},     // zero-padded is not the canonical form
	} {
		if got := TabLabelIsDefault(tc.label, tc.number); got != tc.want {
			t.Fatalf("TabLabelIsDefault(%q, %d) = %v, want %v", tc.label, tc.number, got, tc.want)
		}
	}
}
