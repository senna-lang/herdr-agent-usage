/**
 * Tests for the per-pane activity share calculation.
 */
package limits

import (
	"reflect"
	"testing"
)

func TestDisambiguateLabels_UniqueUntouched(t *testing.T) {
	items := []PaneTokenRow{
		{PaneID: "w6:p1", Label: "Task A"},
		{PaneID: "w6:p2", Label: "Task B"},
	}
	got := DisambiguateLabels(items)
	if !reflect.DeepEqual(got, items) {
		t.Fatalf("got %#v", got)
	}
}

func TestDisambiguateLabels_DuplicateSuffix(t *testing.T) {
	items := []PaneTokenRow{
		{PaneID: "w6:p1", Label: "claude"},
		{PaneID: "w6:pC", Label: "claude"},
	}
	got := DisambiguateLabels(items)
	want := []PaneTokenRow{
		{PaneID: "w6:p1", Label: "claude p1"},
		{PaneID: "w6:pC", Label: "claude pC"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestDisambiguateLabels_OnlyCollidingGroup(t *testing.T) {
	items := []PaneTokenRow{
		{PaneID: "w6:p1", Label: "claude"},
		{PaneID: "w6:pC", Label: "claude"},
		{PaneID: "w6:p3", Label: "Task A"},
	}
	got := DisambiguateLabels(items)
	want := []PaneTokenRow{
		{PaneID: "w6:p1", Label: "claude p1"},
		{PaneID: "w6:pC", Label: "claude pC"},
		{PaneID: "w6:p3", Label: "Task A"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestComputeSharesWithOther_AppendsClosedOther(t *testing.T) {
	rows := []PaneTokenRow{
		{PaneID: "p1", Label: "a", Tokens: 60},
		{PaneID: "p2", Label: "b", Tokens: 40},
	}
	total, panes := ComputeSharesWithOther(rows, 200)
	if total != 200 {
		t.Fatalf("total=%v", total)
	}
	if len(panes) != 3 || panes[0].Label != "a" || panes[1].Label != "b" || panes[2].Label != "closed / other" {
		t.Fatalf("labels=%v", labelsOf(panes))
	}
	if panes[0].SharePercent != 30 {
		t.Fatalf("share[0]=%v want 30", panes[0].SharePercent)
	}
	if panes[2].Tokens != 100 || panes[2].SharePercent != 50 {
		t.Fatalf("other=%+v", panes[2])
	}
}

func TestComputeSharesWithOther_OmitsWhenMatchesOpen(t *testing.T) {
	rows := []PaneTokenRow{
		{PaneID: "p1", Label: "a", Tokens: 60},
		{PaneID: "p2", Label: "b", Tokens: 40},
	}
	_, panes := ComputeSharesWithOther(rows, 100)
	if !reflect.DeepEqual(labelsOf(panes), []string{"a", "b"}) {
		t.Fatalf("labels=%v", labelsOf(panes))
	}
}

func TestComputeSharesWithOther_ClampsBelowOpen(t *testing.T) {
	rows := []PaneTokenRow{
		{PaneID: "p1", Label: "a", Tokens: 60},
		{PaneID: "p2", Label: "b", Tokens: 40},
	}
	total, panes := ComputeSharesWithOther(rows, 40)
	if total != 100 {
		t.Fatalf("total=%v want 100", total)
	}
	if !reflect.DeepEqual(labelsOf(panes), []string{"a", "b"}) {
		t.Fatalf("labels=%v", labelsOf(panes))
	}
}

func TestComputeSharesWithOther_OmitsSubPoint1(t *testing.T) {
	rows := []PaneTokenRow{
		{PaneID: "p1", Label: "a", Tokens: 60},
		{PaneID: "p2", Label: "b", Tokens: 40},
	}
	_, panes := ComputeSharesWithOther(rows, 100.05)
	if !reflect.DeepEqual(labelsOf(panes), []string{"a", "b"}) {
		t.Fatalf("labels=%v", labelsOf(panes))
	}
}

func TestResolveActivityWindowMinutes(t *testing.T) {
	if got := ResolveActivityWindowMinutes(intPtr(300)); got != 300 {
		t.Fatalf("got %d", got)
	}
	if got := ResolveActivityWindowMinutes(intPtr(60)); got != 60 {
		t.Fatalf("got %d", got)
	}
	if got := ResolveActivityWindowMinutes(nil); got != 300 {
		t.Fatalf("got %d want 300", got)
	}
}

func TestWindowStartMs(t *testing.T) {
	if got := WindowStartMs(1_000_000, 10); got != 1_000_000-10*60_000 {
		t.Fatalf("got %d", got)
	}
}

func TestComputePaneActivityShares_ByRatio(t *testing.T) {
	total, panes := ComputePaneActivityShares([]PaneTokenRow{
		{PaneID: "p1", Label: "a", Tokens: 30},
		{PaneID: "p2", Label: "b", Tokens: 70},
	})
	if total != 100 {
		t.Fatalf("total=%v", total)
	}
	want := []PaneActivityShare{
		{PaneID: "p2", Label: "b", Tokens: 70, SharePercent: 70},
		{PaneID: "p1", Label: "a", Tokens: 30, SharePercent: 30},
	}
	if !reflect.DeepEqual(panes, want) {
		t.Fatalf("got %#v want %#v", panes, want)
	}
}

func TestComputePaneActivityShares_DropsNonPositive(t *testing.T) {
	total, panes := ComputePaneActivityShares([]PaneTokenRow{
		{PaneID: "p1", Label: "a", Tokens: 0},
		{PaneID: "p2", Label: "b", Tokens: 50},
		{PaneID: "p3", Label: "c", Tokens: -1},
	})
	if total != 50 || len(panes) != 1 || panes[0].SharePercent != 100 {
		t.Fatalf("total=%v panes=%#v", total, panes)
	}
}

func TestComputePaneActivityShares_AllZero(t *testing.T) {
	total, panes := ComputePaneActivityShares([]PaneTokenRow{
		{PaneID: "p1", Label: "a", Tokens: 0},
	})
	if total != 0 || len(panes) != 0 {
		t.Fatalf("total=%v panes=%#v", total, panes)
	}
}

func TestFormatTokenCount(t *testing.T) {
	cases := map[float64]string{
		512:       "512",
		3400:      "3.4k",
		1_200_000: "1.2M",
		2_000_000: "2M",
	}
	for n, want := range cases {
		if got := FormatTokenCount(n); got != want {
			t.Fatalf("FormatTokenCount(%v)=%q want %q", n, got, want)
		}
	}
}

func labelsOf(panes []PaneActivityShare) []string {
	out := make([]string, len(panes))
	for i, p := range panes {
		out[i] = p.Label
	}
	return out
}

func intPtr(v int) *int { return &v }
