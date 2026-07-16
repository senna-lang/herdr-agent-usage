/**
 * Tests for AttachPaneActivity.
 */
package limits

import (
	"reflect"
	"testing"
)

func baseClaude() ProviderLimits {
	wm := 300
	plan := "Pro"
	return ProviderLimits{
		ProviderID: "claude", Label: "Claude",
		Primary: &LimitWindow{UsedPercentage: 20, WindowMinutes: &wm},
		Source:  "test", FetchedAtMs: 0, PlanType: &plan,
	}
}

func TestAttachPaneActivity_Shares(t *testing.T) {
	open := []OpenPaneSnapshot{
		{PaneID: "w1:p1", Agent: "claude", Label: "claude-a", SessionID: strP("s1"), Cwd: strP("/tmp/a")},
		{PaneID: "w1:p2", Agent: "claude", Label: "claude-b", SessionID: strP("s2"), Cwd: strP("/tmp/b")},
		{PaneID: "w1:p3", Agent: "codex", Label: "codex-only", SessionID: strP("c1"), Cwd: strP("/tmp/c")},
	}
	result := AttachPaneActivity([]ProviderLimits{baseClaude()}, open, 1_700_000_000_000, PaneActivityDeps{
		TokensForPane: func(_ string, pane OpenPaneSnapshot, _, _ int64) float64 {
			if pane.PaneID == "w1:p1" {
				return 75
			}
			if pane.PaneID == "w1:p2" {
				return 25
			}
			return 999
		},
		TotalTokensForProvider: func(string, int64, int64) float64 { return 100 },
	})
	if len(result) != 1 {
		t.Fatalf("len=%d", len(result))
	}
	a := result[0].PaneActivity
	if a == nil || a.WindowMinutes != 300 || a.TotalTokens != 100 {
		t.Fatalf("activity=%+v", a)
	}
	want := []PaneActivityShare{
		{PaneID: "w1:p1", Label: "claude-a", Tokens: 75, SharePercent: 75},
		{PaneID: "w1:p2", Label: "claude-b", Tokens: 25, SharePercent: 25},
	}
	if !reflect.DeepEqual(a.Panes, want) {
		t.Fatalf("panes=%#v want %#v", a.Panes, want)
	}
}

func TestAttachPaneActivity_ClosedOther(t *testing.T) {
	open := []OpenPaneSnapshot{
		{PaneID: "w1:p1", Agent: "claude", Label: "claude-a", SessionID: strP("s1"), Cwd: strP("/tmp/a")},
	}
	result := AttachPaneActivity([]ProviderLimits{baseClaude()}, open, 1_700_000_000_000, PaneActivityDeps{
		TokensForPane:          func(string, OpenPaneSnapshot, int64, int64) float64 { return 50 },
		TotalTokensForProvider: func(string, int64, int64) float64 { return 200 },
	})
	a := result[0].PaneActivity
	if a == nil || a.TotalTokens != 200 {
		t.Fatalf("activity=%+v", a)
	}
	want := []PaneActivityShare{
		{PaneID: "w1:p1", Label: "claude-a", Tokens: 50, SharePercent: 25},
		{PaneID: OtherPaneID, Label: OtherLabel, Tokens: 150, SharePercent: 75},
	}
	if !reflect.DeepEqual(a.Panes, want) {
		t.Fatalf("panes=%#v want %#v", a.Panes, want)
	}
}

func TestAttachPaneActivity_Disambiguate(t *testing.T) {
	open := []OpenPaneSnapshot{
		{PaneID: "w6:p1", Agent: "claude", Label: "claude", SessionID: strP("s1"), Cwd: strP("/tmp/a")},
		{PaneID: "w6:pC", Agent: "claude", Label: "claude", SessionID: strP("s2"), Cwd: strP("/tmp/b")},
	}
	result := AttachPaneActivity([]ProviderLimits{baseClaude()}, open, 1_700_000_000_000, PaneActivityDeps{
		TokensForPane: func(_ string, pane OpenPaneSnapshot, _, _ int64) float64 {
			if pane.PaneID == "w6:p1" {
				return 60
			}
			return 40
		},
		TotalTokensForProvider: func(string, int64, int64) float64 { return 100 },
	})
	labels := []string{}
	for _, p := range result[0].PaneActivity.Panes {
		labels = append(labels, p.Label)
	}
	want := []string{"claude p1", "claude pC"}
	if !reflect.DeepEqual(labels, want) {
		t.Fatalf("labels=%v want %v", labels, want)
	}
}

func TestAttachPaneActivity_NoOpenPanes(t *testing.T) {
	result := AttachPaneActivity([]ProviderLimits{baseClaude()}, nil, 0, PaneActivityDeps{
		TokensForPane:          func(string, OpenPaneSnapshot, int64, int64) float64 { return 10 },
		TotalTokensForProvider: func(string, int64, int64) float64 { return 10 },
	})
	if result[0].PaneActivity != nil {
		t.Fatal("expected no paneActivity")
	}
}

func strP(s string) *string { return &s }
