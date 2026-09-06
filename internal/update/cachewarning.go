/**
 * Collects the red-band prompt-cache panes for the Agent Usage warning line.
 *
 * Cache details otherwise remain sidebar-only. A missing or unresolved session
 * produces no warning rather than guessing at the wrong profile.
 */
package update

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
	"github.com/senna-lang/herdr-agent-usage/internal/providers"
)

// CollectLowCachePanes returns the open panes whose session-cumulative cache
// hit rate is in the red band. It deliberately omits missing cache evidence.
func CollectLowCachePanes(snapshots []limits.OpenPaneSnapshot) []limits.LowCachePane {
	claudeProfiles := limits.ResolvedClaudeProfiles()
	codexProfiles := limits.ResolvedCodexProfiles()
	grokProfiles := limits.ResolvedGrokProfiles()
	openCodeProfiles := limits.ResolvedOpenCodeProfiles()
	resolveProvider := limits.BuildHarnessPaneProviderResolver(
		claudeProfiles, codexProfiles, grokProfiles, openCodeProfiles,
	)

	return collectLowCachePanesWith(
		snapshots,
		herdrcli.GetPaneInfo,
		func(snapshot limits.OpenPaneSnapshot, pane herdrcli.PaneInfo) string {
			naming := herdrcli.GetPaneNaming(pane)
			if label := core.ResolveSidebarTitle(
				naming.PaneLabel, naming.TabLabel, naming.TabNumber, naming.WorkspaceLabel,
			); label != "" {
				return label
			}
			return snapshot.Label
		},
		resolveProvider,
		func(paneID string, pane herdrcli.PaneInfo, providerID string) *core.ContextUsage {
			if pane.Agent == nil {
				return nil
			}
			return resolvePaneUsage(
				paneID, pane, providers.FindProvider(*pane.Agent), providerID,
				claudeProfiles, codexProfiles, grokProfiles, openCodeProfiles,
			)
		},
	)
}

func collectLowCachePanesWith(
	snapshots []limits.OpenPaneSnapshot,
	getPane func(string) herdrcli.PaneInfo,
	labelFor func(limits.OpenPaneSnapshot, herdrcli.PaneInfo) string,
	resolveProvider func(limits.OpenPaneSnapshot) (string, bool),
	resolveUsage func(string, herdrcli.PaneInfo, string) *core.ContextUsage,
) []limits.LowCachePane {
	lowCachePanes := make([]limits.LowCachePane, 0, len(snapshots))
	for _, snapshot := range snapshots {
		providerID, resolved := resolveProvider(snapshot)
		if !resolved {
			continue
		}
		pane := getPane(snapshot.PaneID)
		usage := resolveUsage(snapshot.PaneID, pane, providerID)
		if usage == nil {
			continue
		}
		cache := core.SidebarCache(*usage)
		if cache == nil || core.CacheHitBandFor(cache.HitPercent) != core.CacheHitLow {
			continue
		}

		label := labelFor(snapshot, pane)
		if label == "" {
			label = snapshot.Agent
		}
		if label == "" {
			label = snapshot.PaneID
		}
		lowCachePanes = append(lowCachePanes, limits.LowCachePane{
			Label:      label,
			HitPercent: cache.HitPercent,
		})
	}
	return lowCachePanes
}
