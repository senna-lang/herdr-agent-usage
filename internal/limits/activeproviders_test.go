/**
 * Tests for ActiveProviderSet: open panes -> set of provider ids to display.
 */
package limits

import "testing"

func TestActiveProviderSet_OnlyOpenAgents(t *testing.T) {
	panes := []OpenPaneSnapshot{
		{PaneID: "w1:p1", Agent: "claude"},
		{PaneID: "w1:p2", Agent: "claude"},
		{PaneID: "w1:p3", Agent: "grok"},
	}
	got := ActiveProviderSet(panes)
	if len(got) != 2 || !got["claude"] || !got["grok"] {
		t.Fatalf("got %v, want {claude, grok}", got)
	}
}

func TestActiveProviderSet_EmptyPanes(t *testing.T) {
	got := ActiveProviderSet(nil)
	if got == nil {
		t.Fatal("want non-nil empty set (empty set means: hide all providers)")
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestActiveProviderSet_IgnoresUnknownAgents(t *testing.T) {
	panes := []OpenPaneSnapshot{
		{PaneID: "w1:p1", Agent: ""},
		{PaneID: "w1:p2", Agent: "shell"},
		{PaneID: "w1:p3", Agent: "codex"},
	}
	got := ActiveProviderSet(panes)
	if len(got) != 1 || !got["codex"] {
		t.Fatalf("got %v, want {codex}", got)
	}
}

func TestActiveProviderFilter_FailedPaneQueryFailsOpen(t *testing.T) {
	// When the pane query failed we cannot know what is open: show all
	// providers (nil filter) instead of blanking the panel.
	got := ActiveProviderFilter(nil, false)
	if got != nil {
		t.Fatalf("got %v, want nil (= no filtering)", got)
	}
}

func TestActiveProviderFilter_ConfirmedEmptyHidesAll(t *testing.T) {
	got := ActiveProviderFilter(nil, true)
	if got == nil || len(got) != 0 {
		t.Fatalf("got %v, want non-nil empty set", got)
	}
}

func TestActiveProviderFilter_OKUsesActiveSet(t *testing.T) {
	panes := []OpenPaneSnapshot{{PaneID: "w1:p1", Agent: "claude"}}
	got := ActiveProviderFilter(panes, true)
	if len(got) != 1 || !got["claude"] {
		t.Fatalf("got %v, want {claude}", got)
	}
}

func TestActiveProviderSet_CaseInsensitiveAgentIDs(t *testing.T) {
	panes := []OpenPaneSnapshot{
		{PaneID: "w1:p1", Agent: "Claude"},
		{PaneID: "w1:p2", Agent: "OPENCODE"},
	}
	got := ActiveProviderSet(panes)
	if len(got) != 2 || !got["claude"] || !got["opencode"] {
		t.Fatalf("got %v, want {claude, opencode}", got)
	}
}

func TestActiveProviderSet_AntigravityAndZaiAliases(t *testing.T) {
	got := ActiveProviderSet([]OpenPaneSnapshot{
		{Agent: "AGY"},
		{Agent: "z.ai"},
	})
	if len(got) != 2 || !got["antigravity"] || !got["zai"] {
		t.Fatalf("got %v, want antigravity and zai", got)
	}
}
