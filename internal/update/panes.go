/**
 * Builds OpenPaneSnapshot rows from Herdr's open-agent pane list so the
 * limits pane and the $limit publisher see the same set of panes.
 */
package update

import (
	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

// ListOpenPaneSnapshots lists open agent panes. ok=false means the herdr
// query failed (unknown state), as opposed to a confirmed empty list.
func ListOpenPaneSnapshots() ([]limits.OpenPaneSnapshot, bool) {
	open, ok := herdrcli.ListOpenAgentPanesOK()
	if !ok {
		return nil, false
	}
	snaps := make([]limits.OpenPaneSnapshot, 0, len(open))
	for _, p := range open {
		agent := ""
		if p.Agent != nil {
			agent = *p.Agent
		}
		label := agent
		if p.RowLabel != nil {
			label = *p.RowLabel
		}
		var sid *string
		if p.AgentSession != nil {
			sid = &p.AgentSession.Value
		}
		cwd := herdrcli.PaneSessionCwd(p.PaneInfo)
		snaps = append(snaps, limits.OpenPaneSnapshot{
			PaneID: p.PaneID, Agent: agent, Label: label,
			SessionID: sid, Cwd: cwd,
		})
	}
	return snaps, true
}
