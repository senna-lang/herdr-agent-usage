/**
 * Derives the set of provider ids that have at least one open agent pane,
 * so the limits panel can hide providers not running anywhere in Herdr.
 */
package limits

import "strings"

// ActiveProviderFilter builds the CollectOptions.Only filter from a pane
// query result. When the query failed (paneQueryOK=false) it returns nil —
// fail-open to all providers rather than blanking the panel on a transient
// herdr error. Only a confirmed pane list may hide providers.
func ActiveProviderFilter(openPanes []OpenPaneSnapshot, paneQueryOK bool) map[string]bool {
	if !paneQueryOK {
		return nil
	}
	return ActiveProviderSet(openPanes)
}

// ActiveProviderSet returns the provider ids that have at least one open
// pane. Agent ids match case-insensitively; unknown agents are ignored.
// The result is never nil: an empty set means "no agent panes open".
//
// A Claude pane (any account) activates every configured Claude profile id —
// not just the one that pane happens to belong to — so the panel can show all
// configured accounts side by side for comparison, per the issue's request.
func ActiveProviderSet(openPanes []OpenPaneSnapshot) map[string]bool {
	set := make(map[string]bool)
	hasClaudePane := false
	for _, pane := range openPanes {
		agent := strings.ToLower(pane.Agent)
		if agent == "claude" {
			hasClaudePane = true
			continue
		}
		if providerID, ok := agentToProvider[agent]; ok {
			set[providerID] = true
		}
	}
	if hasClaudePane {
		for _, p := range ResolvedClaudeProfiles() {
			set[p.ID] = true
		}
	}
	return set
}
