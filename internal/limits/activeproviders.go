/**
 * Derives the set of provider ids that have at least one open agent pane,
 * so the limits panel can hide providers not running anywhere in Herdr.
 */
package limits

import "strings"

import "github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
import "github.com/senna-lang/herdr-agent-usage/internal/providers/codex"

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
// A Claude or Codex pane (any account) activates every configured profile id
// of that family — not just the one that pane happens to belong to — so the
// panel can show all configured accounts side by side for comparison.
func ActiveProviderSet(openPanes []OpenPaneSnapshot) map[string]bool {
	profiles := ResolvedClaudeProfiles()
	codexProfiles := ResolvedCodexProfiles()
	return activeProviderSetWith(profiles, codexProfiles, openPanes, func(pane OpenPaneSnapshot) (string, bool) {
		providerID, _, ok := resolveBilledPane(profiles, codexProfiles, pane)
		return providerID, ok
	})
}

func activeProviderSetWith(profiles []claude.ClaudeProfile, codexProfiles []codex.CodexProfile, openPanes []OpenPaneSnapshot, resolve func(OpenPaneSnapshot) (string, bool)) map[string]bool {
	set := make(map[string]bool)
	hasClaudePane := false
	hasCodexPane := false
	for _, pane := range openPanes {
		agent := strings.ToLower(pane.Agent)
		if agent == "claude" {
			hasClaudePane = true
			continue
		}
		if agent == "codex" {
			hasCodexPane = true
			continue
		}
		if providerID, ok := resolve(pane); ok {
			if providerID == "claude" || claude.IsClaudeProviderID(providerID, profiles) {
				hasClaudePane = true
				continue
			}
			if providerID == "codex" || codex.IsCodexProviderID(providerID, codexProfiles) {
				hasCodexPane = true
				continue
			}
			for _, supportedID := range singleCollectorProviderIDs {
				if providerID == supportedID {
					set[providerID] = true
					break
				}
			}
		}
	}
	if hasClaudePane {
		for _, p := range profiles {
			set[p.ID] = true
		}
	}
	if hasCodexPane {
		for _, p := range codexProfiles {
			set[p.ID] = true
		}
	}
	return set
}
