/**
 * Body of the pane.agent_status_changed event: resolves usage for the
 * target pane and refreshes its sidebar label.
 */
package update

import (
	"os"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
	"github.com/senna-lang/herdr-agent-usage/internal/providers"
)

func isSettledStatus(status string) bool {
	return status != "working"
}

// RunUpdate resolves usage for HERDR_PANE_ID and updates custom_status.
// force=true (plugin action) updates even while working.
func RunUpdate(force bool) {
	paneID := os.Getenv("HERDR_PANE_ID")
	if paneID == "" {
		return
	}

	pane := herdrcli.GetPaneInfo(paneID)
	if pane.Agent == nil {
		return
	}

	p := providers.FindProvider(*pane.Agent)
	if p == nil {
		return
	}

	if pane.AgentStatus != nil && !isSettledStatus(*pane.AgentStatus) && !force {
		return
	}

	cwd := pane.ForegroundCwd
	if cwd == nil {
		cwd = pane.Cwd
	}
	usage := p.ResolveUsage(provider.UsageResolveInput{
		Session: pane.AgentSession,
		Cwd:     cwd,
	})
	nowMs := time.Now().UnixMilli()
	collectOptions := limits.DefaultCollectOptions()
	collectOptions.Only = map[string]bool{p.AgentID(): true}
	providerLimits := limits.CollectAllProviderLimits(cwd, nowMs, collectOptions)
	limitText := ""
	if len(providerLimits) > 0 {
		limitText = limits.FormatSidebarLimit(providerLimits[0])
	}
	if limitText == "" {
		herdrcli.ClearMetadataToken(paneID, herdrcli.Source, "limit")
	} else {
		herdrcli.SetMetadataToken(paneID, herdrcli.Source, "limit", limitText)
	}

	if usage == nil {
		herdrcli.ClearMetadataToken(paneID, herdrcli.Source, "context")
		if !force && core.IsAlreadyCleared(paneID) {
			return
		}
		herdrcli.ClearCustomStatus(paneID, herdrcli.Source)
		core.MarkStatusCleared(paneID)
		return
	}

	liveWidth := herdrcli.GetSidebarWidthColumns(paneID)
	sidebarW := core.ResolveSidebarWidth(liveWidth, core.ResolveConfigSidebarWidth())
	maxCols := core.EstimateStatusMaxColumns(&sidebarW, pane.RowLabel)
	statusText := core.FormatUsageStatus(*usage, core.FormatUsageOptions{MaxColumns: maxCols})
	// Herdr 0.7.4+ renders configurable $context tokens. Keep the legacy
	// custom_status write below for Herdr 0.7.0-0.7.3 compatibility.
	herdrcli.SetMetadataToken(paneID, herdrcli.Source, "context", statusText)
	if !core.ShouldWriteStatus(paneID, statusText, force) {
		return
	}
	herdrcli.SetCustomStatus(paneID, herdrcli.Source, statusText)
	core.MarkStatusWritten(paneID, statusText)
}
