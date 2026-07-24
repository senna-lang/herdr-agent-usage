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

// paneCwdForUpdate chooses the directory used to resolve agent-local session
// files. OMP/Pi may put a language-server process in the foreground, whose
// cwd is inside a virtual environment rather than the agent's project. Their
// session fallback is keyed by the pane's project cwd, so prefer it whenever
// Herdr has not supplied a session path. Other agents preserve the existing
// foreground-cwd preference.
func paneCwdForUpdate(pane herdrcli.PaneInfo) *string {
	return herdrcli.PaneSessionCwd(pane)
}

// resolveSidebarAccountLabel picks the text that identifies profile in the
// sidebar's $limit slot once it has been displaced by the account row: the
// real login email when readable, otherwise the profile's own label (which
// defaults to its id, never empty).
func resolveSidebarAccountLabel(profile claude.ClaudeProfile) string {
	if email, ok := limits.AccountEmailFromJSONPath(profile.JSONPath); ok {
		return email
	}
	return profile.Label
}

// reserveColumnsFor shrinks a MaxColumns budget to leave room for a fixed
// "<prefix> · " segment that combineLimitAndContext will join onto the
// context text, so FormatUsageStatus's own truncation still accounts for it.
func reserveColumnsFor(maxColumns *int, prefix string) *int {
	if maxColumns == nil || prefix == "" {
		return maxColumns
	}
	reserved := core.DisplayWidth(prefix) + 3 // " · "
	adjusted := max(*maxColumns-reserved, 3)
	return &adjusted
}

// combineLimitAndContext joins the account's limit text with its context
// status ("5h 88% · ⛁ 14% (136k)") for the sidebar's $context row, once a
// multi-profile Claude pane has moved account identity into $limit's row and
// this is the only row left to carry the limit percentage.
func combineLimitAndContext(limitText, statusText string) string {
	if limitText == "" {
		return statusText
	}
	if statusText == "" {
		return limitText
	}
	return limitText + " · " + statusText
}

// formatSidebarProvider renders the sidebar's agent line: the backend name on
// a pay-as-you-go pane ("deepseek"), and the harness name as a provisional
// fallback until a subscription route supplies its quota-provider label.
//
// The backend replaces the harness rather than joining it — the sidebar is
// too narrow for both, and on a pay-as-you-go pane the backend is the more
// informative half (the harness is already implied by the pane's agent icon).
// It stands in for Herdr's built-in `agent` token, which is why it must carry
// a fallback name when no more specific label is available.
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
	backendID := backendFor(providerID, pane)
	if providerID == "omp" || providerID == "pi" {
		// Without a recorded backend, leaving this blank is more accurate than
		// naming the harness beside an empty or unscoped burn total.
		return backendID
	}
	if backendID != "" {
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

	cwd := paneCwdForUpdate(pane)
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

	billingMode := limits.PaneBillingMode(providerID, snapshot, limits.DefaultBillingDeps())
	limitsProviderID := limits.SubscriptionLimitsProviderID(providerID, snapshot)
	displayProviderID := limits.SubscriptionDisplayProviderID(providerID, snapshot)
	if billingMode == limits.BillingPayAsYouGo {
		totalTokens, totalCostUSD := limits.PaneTotalUsage(providerID, snapshot, nowMs)
		limitText = limits.FormatSidebarBurn(totalTokens, totalCostUSD)
	} else {
		collectOptions := limits.DefaultCollectOptions()
		// Sidebar refresh deliberately collects only this pane's provider and leaves
		// Attach nil, avoiding the heavier cross-pane activity aggregation path.
		collectOptions.Only = map[string]bool{limitsProviderID: true}
		providerLimits := limits.CollectAllProviderLimits(cwd, nowMs, collectOptions)
		if len(providerLimits) > 0 {
			limitText = limits.FormatSidebarLimit(providerLimits[0], nowMs)
		}
	}

	// With 2+ configured Claude accounts, the $limit row's job shifts from
	// "show the limit" to "show which account this pane is" (joined with
	// $provider as "claude · you@example.com") since that's otherwise
	// invisible in the sidebar. The limit percentage moves down into
	// $context instead, as this pane's own account is already unambiguous.
	multiProfile := *pane.Agent == "claude" && len(claudeProfiles) > 1
	accountText := ""
	limitToken := limitText
	if multiProfile {
		if profile, ok := findClaudeProfile(claudeProfiles, providerID); ok {
			accountText = resolveSidebarAccountLabel(profile)
		}
		if accountText != "" {
			limitToken = accountText
		}
	}
	writeMetadataToken(paneID, "limit", limitToken, force)

	// Stands in for Herdr's `agent` token so a pay-as-you-go pane names the
	// backend it is actually billing ("deepseek") instead of the harness.
	providerText := formatSidebarProvider(*pane.Agent, p.AgentID(), snapshot)
	if billingMode == limits.BillingSubscription {
		providerText = displayProviderID
	}
	writeMetadataToken(paneID, "provider", providerText, force)

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

	contextPrefix := ""
	if accountText != "" {
		contextPrefix = limitText
	}

	if usage == nil {
		writeMetadataToken(paneID, "context", contextPrefix, force)
		return
	}

	liveWidth := herdrcli.GetSidebarWidthColumns(paneID)
	sidebarW := core.ResolveSidebarWidth(liveWidth, core.ResolveConfigSidebarWidth())
	maxCols := core.EstimateStatusMaxColumns(&sidebarW, pane.RowLabel)
	maxCols = reserveColumnsFor(maxCols, contextPrefix)
	statusText := core.FormatUsageStatus(*usage, core.FormatUsageOptions{MaxColumns: maxCols})
	writeMetadataToken(paneID, "context", combineLimitAndContext(contextPrefix, statusText), force)
}
