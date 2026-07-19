/**
 * Body of the pane.agent_status_changed event: resolves usage for the
 * target pane and refreshes its sidebar label.
 */
package update

import (
	"fmt"
	"os"
	"strings"
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

func accountLimitProviderID(agent string) string {
	switch strings.ToLower(agent) {
	case "codex":
		return "codex"
	case "grok":
		return "grok"
	case "agy", "antigravity":
		return "antigravity"
	default:
		return ""
	}
}

func accountLabelRemaining(providerID string, providerLimits limits.ProviderLimits) (int, bool) {
	if providerID == "antigravity" {
		// Antigravity collectors order the short 5-hour/session window first.
		// The sidebar should show that immediately actionable allowance instead
		// of the more constrained weekly window.
		if remaining, ok := limits.WindowRemaining(providerLimits.Primary); ok {
			return remaining, true
		}
	}
	return limits.MostConstrainedRemaining(providerLimits)
}

func updateAccountLimitLabel(paneID, agent string, cwd *string) {
	providerID := accountLimitProviderID(agent)
	if providerID == "" {
		return
	}
	nowMs := time.Now().UnixMilli()
	var providerLimits limits.ProviderLimits
	switch providerID {
	case "codex":
		providerLimits = limits.CollectCodexLimits(cwd, nowMs)
	case "grok":
		providerLimits = limits.CollectGrokLimits(nowMs, limits.CollectGrokLimitsOptions{})
	case "antigravity":
		providerLimits = limits.CollectAntigravityLimits(cwd, nowMs, limits.CollectAntigravityLimitsOptions{})
	}
	remaining, ok := accountLabelRemaining(providerID, providerLimits)
	if !ok {
		herdrcli.ClearDisplayAgent(paneID, herdrcli.Source)
		return
	}
	herdrcli.SetDisplayAgent(paneID, herdrcli.Source, fmt.Sprintf("%s %d%%", agent, remaining))
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

	if pane.AgentStatus != nil && !isSettledStatus(*pane.AgentStatus) && !force {
		return
	}

	cwd := pane.ForegroundCwd
	if cwd == nil {
		cwd = pane.Cwd
	}
	updateAccountLimitLabel(paneID, *pane.Agent, cwd)

	p := providers.FindProvider(*pane.Agent)
	if p == nil {
		return
	}
	usage := p.ResolveUsage(provider.UsageResolveInput{
		Session: pane.AgentSession,
		Cwd:     cwd,
	})

	if usage == nil {
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
	if !core.ShouldWriteStatus(paneID, statusText, force) {
		return
	}
	herdrcli.SetCustomStatus(paneID, herdrcli.Source, statusText)
	core.MarkStatusWritten(paneID, statusText)
}
