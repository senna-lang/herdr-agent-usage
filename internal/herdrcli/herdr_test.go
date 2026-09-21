/**
 * Tests for BuildOpenAgentPanes — pure rowLabel resolution.
 */
package herdrcli

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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

// writeFakeHerdr writes an executable stand-in for the herdr CLI.
func writeFakeHerdr(t *testing.T, dir, script string) string {
	t.Helper()
	path := filepath.Join(dir, "herdr")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHerdrBin_UsesRunnableHERDRBinPath(t *testing.T) {
	bin := writeFakeHerdr(t, t.TempDir(), "#!/bin/sh\n")
	t.Setenv("HERDR_BIN_PATH", bin)
	if got := herdrBin(); got != bin {
		t.Fatalf("got %q want %q", got, bin)
	}
}

func TestHerdrBin_FallsBackToPATHWhenHERDRBinPathIsStale(t *testing.T) {
	// Herdr reports a replaced or removed binary as `<path> (deleted)`, and a
	// package manager that rotates version directories leaves the same kind of
	// dead path behind.
	root := t.TempDir()
	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "removed version directory", path: filepath.Join(root, "0.8.2", "herdr")},
		{name: "removed binary in an existing directory", path: filepath.Join(root, "herdr")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HERDR_BIN_PATH", tt.path)
			if got := herdrBin(); got != "herdr" {
				t.Fatalf("got %q want herdr", got)
			}
		})
	}
}

func TestHerdrBin_FallsBackToPATHWhenHERDRBinPathIsNotExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "herdr")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", path)
	if got := herdrBin(); got != "herdr" {
		t.Fatalf("got %q want herdr", got)
	}
}

func TestHerdrBin_DefaultsToPATHWhenUnset(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "")
	if got := herdrBin(); got != "herdr" {
		t.Fatalf("got %q want herdr", got)
	}
}

func TestSpawnHerdr_RunsPATHFallbackWhenHERDRBinPathIsStale(t *testing.T) {
	binDir := t.TempDir()
	writeFakeHerdr(t, binDir, "#!/bin/sh\nprintf 'pane-list-ok'\n")
	t.Setenv("PATH", binDir)
	t.Setenv("HERDR_BIN_PATH", filepath.Join(t.TempDir(), "0.8.2", "herdr"))

	got, ok := spawnHerdr("agent", "list")
	if !ok {
		t.Fatal("spawnHerdr failed")
	}
	if got != "pane-list-ok" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestSpawnHerdr_ReportsSpawnFailureOnStderr(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "")

	var ok bool
	out := captureStderr(t, func() {
		_, ok = spawnHerdr("pane", "get", "w6:p1")
	})
	if ok {
		t.Fatal("expected the herdr call to fail")
	}
	if !strings.Contains(out, "herdr pane get failed") {
		t.Fatalf("stderr = %q", out)
	}
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what fn
// wrote. The package's tests are not parallel and already swap os.Stderr, so
// the global redirect is safe here.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = write
	// Restore from a defer too, so an assertion that aborts fn cannot leak the
	// swapped descriptor into the next test.
	defer func() { os.Stderr = stderr }()
	fn()
	os.Stderr = stderr
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// resetHerdrBinFallbackNotice clears the process-wide once so a test observes
// the first-call notice no matter which test ran before it.
func resetHerdrBinFallbackNotice(t *testing.T) {
	t.Helper()
	herdrBinFallbackNoticeOnce = sync.Once{}
	t.Cleanup(func() { herdrBinFallbackNoticeOnce = sync.Once{} })
}

func TestHerdrBin_ReportsFallbackOncePerProcess(t *testing.T) {
	// A single sidebar refresh makes roughly ten callbacks, all of which fall
	// back in a stale-HERDR_BIN_PATH environment, so the notice must not repeat
	// once per call.
	resetHerdrBinFallbackNotice(t)
	t.Setenv("HERDR_BIN_PATH", filepath.Join(t.TempDir(), "0.8.2", "herdr"))

	out := captureStderr(t, func() {
		for i := 0; i < 3; i++ {
			if got := herdrBin(); got != "herdr" {
				t.Fatalf("got %q want herdr", got)
			}
		}
	})
	if got := strings.Count(out, "is not runnable"); got != 1 {
		t.Fatalf("fallback notices = %d, want 1; stderr = %q", got, out)
	}
}
