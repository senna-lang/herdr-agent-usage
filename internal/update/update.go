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

type metadataTokenWriter struct {
	set   func(paneID, source, name, value string) bool
	clear func(paneID, source, name string) bool
}

var herdrMetadataTokenWriter = metadataTokenWriter{
	set:   herdrcli.SetMetadataToken,
	clear: herdrcli.ClearMetadataToken,
}

func writeMetadataToken(paneID, name, value string, force bool) {
	writeMetadataTokenWith(herdrMetadataTokenWriter, paneID, name, value, force)
}

func writeMetadataTokenWith(writer metadataTokenWriter, paneID, name, value string, force bool) {
	if !core.ShouldWriteToken(paneID, name, value, force) {
		return
	}
	ok := false
	if value == "" {
		ok = writer.clear(paneID, herdrcli.Source, name)
	} else {
		ok = writer.set(paneID, herdrcli.Source, name, value)
	}
	if ok {
		core.MarkTokenWritten(paneID, name, value)
	}
}

// RunUpdate resolves usage for HERDR_PANE_ID and updates its sidebar tokens.
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
	limitText := ""
	// Subscription limits only apply when the pane's session is billed against
	// a subscription plan; a pay-as-you-go backend (API key, custom base_url)
	// has no plan window to report against, so the row shows the pane's
	// whole-session token/cost total instead.
	var sid *string
	if pane.AgentSession != nil {
		sid = &pane.AgentSession.Value
	}
	snapshot := limits.OpenPaneSnapshot{PaneID: paneID, Agent: *pane.Agent, SessionID: sid, Cwd: cwd}
	if limits.PaneBillingMode(p.AgentID(), snapshot, limits.DefaultBillingDeps()) == limits.BillingPayAsYouGo {
		totalTokens, totalCostUSD := limits.PaneTotalUsage(p.AgentID(), snapshot, nowMs)
		limitText = limits.FormatSidebarBurn(totalTokens, totalCostUSD)
	} else {
		collectOptions := limits.DefaultCollectOptions()
		// Sidebar refresh deliberately collects only this pane's provider and leaves
		// Attach nil, avoiding the heavier cross-pane activity aggregation path.
		collectOptions.Only = map[string]bool{p.AgentID(): true}
		providerLimits := limits.CollectAllProviderLimits(cwd, nowMs, collectOptions)
		if len(providerLimits) > 0 {
			limitText = limits.FormatSidebarLimit(providerLimits[0], nowMs)
		}
	}
	writeMetadataToken(paneID, "limit", limitText, force)

	if usage == nil {
		writeMetadataToken(paneID, "context", "", force)
		return
	}

	liveWidth := herdrcli.GetSidebarWidthColumns(paneID)
	sidebarW := core.ResolveSidebarWidth(liveWidth, core.ResolveConfigSidebarWidth())
	maxCols := core.EstimateStatusMaxColumns(&sidebarW, pane.RowLabel)
	statusText := core.FormatUsageStatus(*usage, core.FormatUsageOptions{MaxColumns: maxCols})
	writeMetadataToken(paneID, "context", statusText, force)
}
