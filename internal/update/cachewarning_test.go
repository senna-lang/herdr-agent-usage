/**
 * Verifies that Agent Usage warnings include only panes with red-band
 * session-cumulative prompt-cache hit rates.
 */
package update

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

func TestCollectLowCachePanesWith_ReportsOnlyRedBandPanes(t *testing.T) {
	paneAgent := "claude"
	panes := []limits.OpenPaneSnapshot{
		{PaneID: "low", Agent: paneAgent, Label: "research"},
		{PaneID: "mid", Agent: paneAgent, Label: "coding"},
		{PaneID: "unknown", Agent: paneAgent, Label: "other"},
		{PaneID: "unresolved", Agent: paneAgent, Label: "ignored"},
	}

	got := collectLowCachePanesWith(
		panes,
		func(string) herdrcli.PaneInfo { return herdrcli.PaneInfo{Agent: &paneAgent} },
		func(snapshot limits.OpenPaneSnapshot, _ herdrcli.PaneInfo) string {
			if snapshot.PaneID == "low" {
				return "workspace・research"
			}
			return snapshot.Label
		},
		func(snapshot limits.OpenPaneSnapshot) (string, bool) {
			return snapshot.PaneID, snapshot.PaneID != "unresolved"
		},
		func(_ string, _ herdrcli.PaneInfo, providerID string) *core.ContextUsage {
			switch providerID {
			case "low":
				return &core.ContextUsage{Cache: &core.CacheUsage{HitPercent: 99}, SessionCache: &core.CacheUsage{HitPercent: 43.1}}
			case "mid":
				return &core.ContextUsage{SessionCache: &core.CacheUsage{HitPercent: 50}}
			default:
				return &core.ContextUsage{}
			}
		},
	)

	if len(got) != 1 {
		t.Fatalf("warnings=%+v", got)
	}
	if got[0].Label != "workspace・research" || got[0].HitPercent != 43.1 {
		t.Fatalf("warning=%+v", got[0])
	}
}
