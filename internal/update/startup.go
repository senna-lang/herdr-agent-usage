/**
 * Republishes sidebar tokens for every open agent pane after Herdr
 * restores the session (and again after live handoff).
 *
 * Server-owned metadata tokens do not survive a cold restart. The event
 * path only sees HERDR_PANE_ID for one pane, and PublishCollectedLimits
 * writes $limit only. Startup therefore reuses RunUpdateForPane so $title,
 * $provider, $limit, and $context — including the post-compaction label
 * and pay-as-you-go burn — come back in one pass. force is false so the
 * live-token dedupe still applies when handoff leaves tokens already set.
 */
package update

import "github.com/senna-lang/herdr-agent-usage/internal/herdrcli"

// RepublishOpenAgentPanes restores sidebar tokens for every open agent pane.
func RepublishOpenAgentPanes() {
	republishOpenAgentPanesWith(herdrcli.ListOpenAgentPanesOK, RunUpdateForPane)
}

func republishOpenAgentPanesWith(
	list func() ([]herdrcli.OpenAgentPane, bool),
	updatePane func(paneID string, force bool),
) {
	panes, ok := list()
	if !ok {
		return
	}
	for _, pane := range panes {
		if pane.PaneID == "" {
			continue
		}
		updatePane(pane.PaneID, false)
	}
}
