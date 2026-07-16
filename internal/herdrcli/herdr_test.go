/**
 * Tests for BuildOpenAgentPanes — pure rowLabel resolution.
 */
package herdrcli

import (
	"reflect"
	"testing"
)

func TestBuildOpenAgentPanes_TabFallback(t *testing.T) {
	panes := []RawPaneListEntry{
		{PaneID: "w6:p1", Agent: "claude", TabID: "w6:t1"},
		{PaneID: "w6:p2", Agent: "grok", TabID: "w6:t2"},
	}
	tabLabels := map[string]string{"w6:t1": "Task A", "w6:t2": "Task B"}
	out := BuildOpenAgentPanes(panes, tabLabels)
	got := []string{deref(out[0].RowLabel), deref(out[1].RowLabel)}
	want := []string{"Task A", "Task B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestBuildOpenAgentPanes_PaneRenameWins(t *testing.T) {
	panes := []RawPaneListEntry{
		{PaneID: "w6:pC", Agent: "claude", Label: "TaskD", TabID: "w6:t3"},
	}
	tabLabels := map[string]string{"w6:t3": "Task C"}
	out := BuildOpenAgentPanes(panes, tabLabels)
	if deref(out[0].RowLabel) != "TaskD" {
		t.Fatalf("%v", out[0].RowLabel)
	}
}

func TestBuildOpenAgentPanes_BareAgent(t *testing.T) {
	panes := []RawPaneListEntry{{PaneID: "w6:p1", Agent: "claude", TabID: "w6:t9"}}
	out := BuildOpenAgentPanes(panes, map[string]string{})
	if deref(out[0].RowLabel) != "claude" {
		t.Fatalf("%v", out[0].RowLabel)
	}
}

func TestBuildOpenAgentPanes_ExcludesNoAgent(t *testing.T) {
	panes := []RawPaneListEntry{{PaneID: "w6:p1"}, {PaneID: "w6:p2", Agent: ""}}
	if len(BuildOpenAgentPanes(panes, nil)) != 0 {
		t.Fatal("expected empty")
	}
}

func TestBuildOpenAgentPanes_SharedTab(t *testing.T) {
	panes := []RawPaneListEntry{
		{PaneID: "w6:p2", Agent: "grok", TabID: "w6:t2"},
		{PaneID: "w6:p3", Agent: "codex", TabID: "w6:t2"},
		{PaneID: "w6:p4", Agent: "opencode", TabID: "w6:t2"},
	}
	tabLabels := map[string]string{"w6:t2": "Task B"}
	out := BuildOpenAgentPanes(panes, tabLabels)
	for _, p := range out {
		if deref(p.RowLabel) != "Task B" {
			t.Fatalf("%+v", p)
		}
	}
}
