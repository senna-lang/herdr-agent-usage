/**
 * Body of the pane.agent_status_changed event: resolves usage for the
 * target pane and refreshes its sidebar label.
 */
package update

import (
	"os"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
	"github.com/senna-lang/herdr-agent-usage/internal/providers"
	claudeprovider "github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
)

// findClaudeProfile looks up one resolved profile by provider id.
func findClaudeProfile(profiles []claude.ClaudeProfile, id string) (claude.ClaudeProfile, bool) {
	for _, p := range profiles {
		if p.ID == id {
			return p, true
		}
	}
	return claude.ClaudeProfile{}, false
}

func isSettledStatus(status string) bool {
	return status != "working"
}

// formatSidebarProvider renders the sidebar's agent line: the backend name on
// a pay-as-you-go pane ("deepseek"), the harness name otherwise ("opencode").
//
// The backend replaces the harness rather than joining it — the sidebar is
// too narrow for both, and on a pay-as-you-go pane the backend is the more
// informative half (the harness is already implied by the pane's agent icon).
// It stands in for Herdr's built-in `agent` token, which is why it must carry
// the harness name in the subscription case.
func formatSidebarProvider(agentName, providerID string, pane limits.OpenPaneSnapshot) string {
	return formatSidebarProviderWith(limits.PaneBackendID, agentName, providerID, pane)
}

func formatSidebarProviderWith(
	backendFor func(string, limits.OpenPaneSnapshot) string,
	agentName, providerID string,
	pane limits.OpenPaneSnapshot,
) string {
	if agentName == "" {
		return ""
	}
	if backendID := backendFor(providerID, pane); backendID != "" {
		return backendID
	}
	return agentName
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

	// Resolve which specific provider this pane belongs to. For claude this is
	// profile-aware (session-transcript match across configured accounts);
	// other agents resolve 1:1 with p.AgentID() as before. ok=false only
	// happens for an ambiguous multi-profile claude pane.
	claudeProfiles := limits.ResolvedClaudeProfiles()
	providerID, resolved := limits.BuildClaudePaneProviderResolver(claudeProfiles)(snapshot)
	if !resolved {
		// Cannot tell which account this pane belongs to: clear rather than
		// guess into the wrong account's limits/tokens.
		writeMetadataToken(paneID, "limit", "", force)
		writeMetadataToken(paneID, "provider", formatSidebarProvider(*pane.Agent, p.AgentID(), snapshot), force)
		writeMetadataToken(paneID, "context", "", force)
		return
	}

	if limits.PaneBillingMode(providerID, snapshot, limits.DefaultBillingDeps()) == limits.BillingPayAsYouGo {
		totalTokens, totalCostUSD := limits.PaneTotalUsage(providerID, snapshot, nowMs)
		limitText = limits.FormatSidebarBurn(totalTokens, totalCostUSD)
	} else {
		collectOptions := limits.DefaultCollectOptions()
		// Sidebar refresh deliberately collects only this pane's provider and leaves
		// Attach nil, avoiding the heavier cross-pane activity aggregation path.
		collectOptions.Only = map[string]bool{providerID: true}
		providerLimits := limits.CollectAllProviderLimits(cwd, nowMs, collectOptions)
		if len(providerLimits) > 0 {
			limitText = limits.FormatSidebarLimit(providerLimits[0], nowMs)
		}
	}
	writeMetadataToken(paneID, "limit", limitText, force)

	// Stands in for Herdr's `agent` token so a pay-as-you-go pane names the
	// backend it is actually billing ("deepseek") instead of the harness.
	writeMetadataToken(paneID, "provider", formatSidebarProvider(*pane.Agent, p.AgentID(), snapshot), force)

	// Context tokens: claude is read from its resolved profile's own transcript
	// root (bypassing the registry's default-root lookup) so a non-default
	// account's context display doesn't fall back to ~/.claude/projects.
	var usage *core.ContextUsage
	if *pane.Agent == "claude" {
		if profile, ok := findClaudeProfile(claudeProfiles, providerID); ok && sid != nil {
			if transcript := claudeprovider.ResolveUsageForSessionIn(profile.ProjectsRoot, *sid); transcript != nil {
				u := claudeprovider.ToContextUsage(*transcript)
				usage = &u
			}
		}
	} else {
		usage = p.ResolveUsage(provider.UsageResolveInput{
			Session: pane.AgentSession,
			Cwd:     cwd,
		})
	}

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
