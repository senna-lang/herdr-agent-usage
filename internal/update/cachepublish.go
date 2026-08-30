/**
 * Fans session-specific cache diagnostics out to open panes.
 *
 * Unlike account-level limits, cache data belongs to each session. This
 * publisher reads every open pane's resolved usage but writes only `$cache`.
 */
package update

import (
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
	"github.com/senna-lang/herdr-agent-usage/internal/providers"
)

// PublishOpenPaneCaches refreshes the $cache token for every open agent pane.
// It deliberately does not re-collect provider limits or alter $context.
func PublishOpenPaneCaches(now time.Time) {
	claudeProfiles := limits.ResolvedClaudeProfiles()
	codexProfiles := limits.ResolvedCodexProfiles()
	grokProfiles := limits.ResolvedGrokProfiles()
	openCodeProfiles := limits.ResolvedOpenCodeProfiles()
	resolveProvider := limits.BuildHarnessPaneProviderResolver(
		claudeProfiles, codexProfiles, grokProfiles, openCodeProfiles,
	)

	publishOpenPaneCachesWith(
		herdrMetadataTokenWriter,
		ListOpenPaneSnapshots,
		herdrcli.GetPaneInfo,
		resolveProvider,
		func(paneID string, pane herdrcli.PaneInfo, providerID string) *core.ContextUsage {
			if pane.Agent == nil {
				return nil
			}
			return resolvePaneUsage(
				paneID,
				pane,
				providers.FindProvider(*pane.Agent),
				providerID,
				claudeProfiles,
				codexProfiles,
				grokProfiles,
				openCodeProfiles,
			)
		},
		now,
	)
}

func publishOpenPaneCachesWith(
	writer metadataTokenWriter,
	listSnapshots func() ([]limits.OpenPaneSnapshot, bool),
	getPane func(string) herdrcli.PaneInfo,
	resolveProvider func(limits.OpenPaneSnapshot) (string, bool),
	resolveUsage func(string, herdrcli.PaneInfo, string) *core.ContextUsage,
	now time.Time,
) {
	snapshots, ok := listSnapshots()
	if !ok {
		return
	}
	for _, snapshot := range snapshots {
		pane := getPane(snapshot.PaneID)
		providerID, resolved := resolveProvider(snapshot)
		var usage *core.ContextUsage
		if resolved {
			usage = resolveUsage(snapshot.PaneID, pane, providerID)
		}
		writePaneCacheToken(writer, pane, snapshot.PaneID, usage, now)
	}
}

func writePaneCacheToken(
	writer metadataTokenWriter,
	pane herdrcli.PaneInfo,
	paneID string,
	usage *core.ContextUsage,
	now time.Time,
) {
	cacheText := ""
	if usage != nil && usage.Cache != nil {
		cacheText = core.FormatCacheStatus(*usage.Cache, now.Unix())
	}
	retainExistingOnEmpty := pane.AgentStatus != nil && *pane.AgentStatus == "working"
	if retainExistingOnEmpty && cacheText == "" {
		return
	}
	writeMetadataTokenWith(writer, pane.Tokens, paneID, "cache", cacheText, false)
}
