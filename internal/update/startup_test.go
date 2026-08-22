/**
 * Tests for the startup republish of sidebar tokens across open agent panes.
 *
 * After a Herdr restart the server-side tokens are gone. This path must
 * visit every listed pane once, never force-write (so live-token dedupe
 * still applies on handoff), and write nothing when the pane query failed.
 */
package update

import (
	"reflect"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
)

func TestRepublishOpenAgentPanesWith_UpdatesEachListedPane(t *testing.T) {
	var got []string
	var forced []bool
	republishOpenAgentPanesWith(
		func() ([]herdrcli.OpenAgentPane, bool) {
			return []herdrcli.OpenAgentPane{
				{PaneID: "w1:p1"},
				{PaneID: "w1:p2"},
			}, true
		},
		func(paneID string, force bool) {
			got = append(got, paneID)
			forced = append(forced, force)
		},
	)
	want := []string{"w1:p1", "w1:p2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updated %v, want %v", got, want)
	}
	for i, force := range forced {
		if force {
			t.Fatalf("pane %s: startup must not force-write tokens", got[i])
		}
	}
}

func TestRepublishOpenAgentPanesWith_FailedListWritesNothing(t *testing.T) {
	called := false
	republishOpenAgentPanesWith(
		func() ([]herdrcli.OpenAgentPane, bool) {
			return []herdrcli.OpenAgentPane{{PaneID: "w1:p1"}}, false
		},
		func(string, bool) { called = true },
	)
	if called {
		t.Fatal("a failed pane query must not republish tokens")
	}
}

func TestRepublishOpenAgentPanesWith_EmptyListWritesNothing(t *testing.T) {
	called := false
	republishOpenAgentPanesWith(
		func() ([]herdrcli.OpenAgentPane, bool) {
			return nil, true
		},
		func(string, bool) { called = true },
	)
	if called {
		t.Fatal("no open agent panes must not republish tokens")
	}
}

func TestRepublishOpenAgentPanesWith_SkipsEmptyPaneID(t *testing.T) {
	var got []string
	republishOpenAgentPanesWith(
		func() ([]herdrcli.OpenAgentPane, bool) {
			return []herdrcli.OpenAgentPane{
				{PaneID: ""},
				{PaneID: "w1:p3"},
			}, true
		},
		func(paneID string, force bool) {
			got = append(got, paneID)
		},
	)
	if !reflect.DeepEqual(got, []string{"w1:p3"}) {
		t.Fatalf("updated %v, want [w1:p3]", got)
	}
}
