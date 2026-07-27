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

// Real `herdr tab get` / `herdr workspace get` payloads, kept verbatim so the
// parsers fail loudly if the upstream JSON shape drifts.
const liveTabJSON = `{"id":"cli:tab:get","result":{"tab":{"agent_status":"idle","focused":true,` +
	`"label":"herdr-agent-usage","number":1,"pane_count":3,"tab_id":"w6:t1","workspace_id":"w6"},` +
	`"type":"tab_info"}}`

const liveWorkspaceJSON = `{"id":"cli:workspace:get","result":{"type":"workspace_info","workspace":{` +
	`"active_tab_id":"w6:t1","agent_status":"idle","focused":true,"label":"herdr-agent-usage",` +
	`"number":1,"pane_count":3,"tab_count":1,"workspace_id":"w6"}}}`

func TestParseTabInfo_LivePayload(t *testing.T) {
	got := parseTabInfo(liveTabJSON)
	if got != (TabInfo{Label: "herdr-agent-usage", Number: 1}) {
		t.Fatalf("%+v", got)
	}
}

func TestParseTabInfo_MalformedYieldsZero(t *testing.T) {
	for _, in := range []string{"", "not json", `{"result":{}}`, `{"result":null}`} {
		if got := parseTabInfo(in); got != (TabInfo{}) {
			t.Fatalf("input %q: %+v", in, got)
		}
	}
}

func TestParseWorkspaceInfo_LivePayload(t *testing.T) {
	if got := parseWorkspaceInfo(liveWorkspaceJSON); got.Label != "herdr-agent-usage" {
		t.Fatalf("%+v", got)
	}
}

func TestParseWorkspaceInfo_MalformedYieldsZero(t *testing.T) {
	for _, in := range []string{"", "not json", `{"result":{}}`} {
		if got := parseWorkspaceInfo(in); got != (WorkspaceInfo{}) {
			t.Fatalf("input %q: %+v", in, got)
		}
	}
}

func namingPane(label, tabID, workspaceID string) PaneInfo {
	return PaneInfo{Label: &label, TabID: &tabID, WorkspaceID: &workspaceID}
}

func TestBuildPaneNaming_RenamedTabSkipsWorkspaceRoundTrip(t *testing.T) {
	workspaceCalls := 0
	got := buildPaneNaming(
		namingPane("Task A", "w6:t1", "w6"),
		func(string) TabInfo { return TabInfo{Label: "herdr-agent-usage", Number: 1} },
		func(string) WorkspaceInfo { workspaceCalls++; return WorkspaceInfo{Label: "unused"} },
	)
	if workspaceCalls != 0 {
		t.Fatalf("workspace fetched %d times for a self-naming tab", workspaceCalls)
	}
	want := PaneNaming{PaneLabel: "Task A", TabLabel: "herdr-agent-usage", TabNumber: 1}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestBuildPaneNaming_DefaultTabFetchesWorkspace(t *testing.T) {
	var gotWorkspaceID string
	got := buildPaneNaming(
		namingPane("", "wA:t1", "wA"),
		func(string) TabInfo { return TabInfo{Label: "1", Number: 1} },
		func(id string) WorkspaceInfo { gotWorkspaceID = id; return WorkspaceInfo{Label: "logosyncs"} },
	)
	if gotWorkspaceID != "wA" {
		t.Fatalf("workspace id %q", gotWorkspaceID)
	}
	want := PaneNaming{TabLabel: "1", TabNumber: 1, WorkspaceLabel: "logosyncs"}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestBuildPaneNaming_NilPointersDoNotPanic(t *testing.T) {
	var gotTabID string
	got := buildPaneNaming(
		PaneInfo{},
		func(id string) TabInfo { gotTabID = id; return TabInfo{} },
		func(string) WorkspaceInfo { return WorkspaceInfo{} },
	)
	if gotTabID != "" || got != (PaneNaming{}) {
		t.Fatalf("tabID=%q naming=%+v", gotTabID, got)
	}
}
