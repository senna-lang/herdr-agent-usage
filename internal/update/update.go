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
	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
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

func findCodexProfile(profiles []codex.CodexProfile, id string) (codex.CodexProfile, bool) {
	for _, p := range profiles {
		if p.ID == id {
			return p, true
		}
	}
	return codex.CodexProfile{}, false
}

func findGrokProfile(profiles []grok.GrokProfile, id string) (grok.GrokProfile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return grok.GrokProfile{}, false
}

func findOpenCodeProfile(profiles []opencode.OpenCodeProfile, id string) (opencode.OpenCodeProfile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return opencode.OpenCodeProfile{}, false
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

// formatSidebarBillingTokens renders the sidebar's provider/limit pair after
// billing identity and usage have been resolved. Subscription panes name the
// quota provider and show its numerically shortest limit window. Pay-as-you-go
// panes keep the resolved backend name and show backend-scoped session burn,
// including USD only when the harness supplied a non-zero cost.
func formatSidebarBillingTokens(
	billingMode limits.BillingMode,
	fallbackProviderText, displayProviderID string,
	providerLimits *limits.ProviderLimits,
	totalTokens, totalCostUSD float64,
	nowMs int64,
	percent core.LimitPercent,
) (providerText, limitText string) {
	providerText = fallbackProviderText
	if billingMode != limits.BillingPayAsYouGo {
		if billingMode == limits.BillingSubscription {
			providerText = displayProviderID
		}
		if providerLimits != nil {
			limitText = limits.FormatSidebarLimit(*providerLimits, nowMs, percent)
		}
		return providerText, limitText
	}
	if billingMode == limits.BillingPayAsYouGo {
		limitText = limits.FormatSidebarBurn(totalTokens, totalCostUSD)
	}
	return providerText, limitText
}

type metadataTokenWriter struct {
	set   func(paneID, source, name, value string) bool
	clear func(paneID, source, name string) bool
}

var herdrMetadataTokenWriter = metadataTokenWriter{
	set:   herdrcli.SetMetadataToken,
	clear: herdrcli.ClearMetadataToken,
}

// writeMetadataToken applies the pane's metadata write policy before deciding
// whether the current value needs a Herdr update. Live updates retain a
// last-known-good value when collection temporarily yields an empty value.
func writeMetadataToken(current map[string]string, paneID, name, value string, force, retainExistingOnEmpty bool) {
	if retainExistingOnEmpty && value == "" {
		return
	}
	writeMetadataTokenWith(herdrMetadataTokenWriter, current, paneID, name, value, force)
}

func writeMetadataTokenWith(writer metadataTokenWriter, current map[string]string, paneID, name, value string, force bool) {
	if !core.ShouldWriteToken(current, name, value, force) {
		return
	}
	if value == "" {
		writer.clear(paneID, herdrcli.Source, name)
	} else {
		writer.set(paneID, herdrcli.Source, name, value)
	}
}

// RunUpdate resolves usage for HERDR_PANE_ID and refreshes its sidebar tokens,
// including while the agent is working. force bypasses unchanged-value checks.
func RunUpdate(force bool) {
	RunUpdateForPane(os.Getenv("HERDR_PANE_ID"), force)
}

// RunUpdateForPane resolves usage for paneID and refreshes its sidebar tokens.
// force bypasses unchanged-value checks.
func RunUpdateForPane(paneID string, force bool) {
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

	// A working pane can briefly have no fresh limit or context value while its
	// collector or transcript is between complete records. Keep its last-known-
	// good metadata until a positive update arrives; settled and forced updates
	// may still clear stale values.
	retainExistingOnEmpty := !force && pane.AgentStatus != nil && *pane.AgentStatus == "working"
	// Row 1 stands in for Herdr's built-in `tab`/`pane` tokens, which render
	// blank whenever a tab keeps its auto-assigned numeric label and the
	// pane was never renamed. Fall back through the workspace ("space")
	// name so the row is never empty.
	naming := herdrcli.GetPaneNaming(pane)
	title := core.ResolveSidebarTitle(naming.PaneLabel, naming.TabLabel, naming.TabNumber, naming.WorkspaceLabel)
	writeMetadataToken(pane.Tokens, paneID, "title", title, force, retainExistingOnEmpty)

	cwd := paneCwdForUpdate(pane)
	nowMs := time.Now().UnixMilli()
	// Subscription limits only apply when the pane's session is billed against
	// a subscription plan; a pay-as-you-go backend (API key, custom base_url)
	// has no plan window to report against, so the row shows the pane's
	// whole-session token/cost total instead.
	var sid *string
	if pane.AgentSession != nil {
		sid = &pane.AgentSession.Value
	}
	snapshot := limits.OpenPaneSnapshot{PaneID: paneID, Agent: *pane.Agent, SessionID: sid, Cwd: cwd}

	// Resolve which specific provider this pane belongs to. A multi-profile
	// pane with no unique session-store match is intentionally left unresolved.
	claudeProfiles := limits.ResolvedClaudeProfiles()
	codexProfiles := limits.ResolvedCodexProfiles()
	grokProfiles := limits.ResolvedGrokProfiles()
	openCodeProfiles := limits.ResolvedOpenCodeProfiles()
	providerID, resolved := limits.BuildHarnessPaneProviderResolver(
		claudeProfiles, codexProfiles, grokProfiles, openCodeProfiles,
	)(snapshot)
	if !resolved {
		// Cannot tell which account this pane belongs to: clear rather than
		// guess into the wrong account's limits/tokens.
		writeMetadataToken(pane.Tokens, paneID, "limit", "", force, retainExistingOnEmpty)
		writeMetadataToken(pane.Tokens, paneID, "provider", formatSidebarProvider(*pane.Agent, p.AgentID(), snapshot), force, retainExistingOnEmpty)
		writeMetadataToken(pane.Tokens, paneID, "context", "", force, retainExistingOnEmpty)
		return
	}

	billingMode := limits.PaneBillingMode(providerID, snapshot, limits.DefaultBillingDeps())
	limitsProviderID := limits.SubscriptionLimitsProviderID(providerID, snapshot)
	displayProviderID := limits.SubscriptionDisplayProviderID(providerID, snapshot)
	var providerLimits *limits.ProviderLimits
	var totalTokens, totalCostUSD float64
	if billingMode == limits.BillingPayAsYouGo {
		totalTokens, totalCostUSD = limits.PaneTotalUsage(providerID, snapshot, nowMs)
	} else {
		collectOptions := limits.DefaultCollectOptions()
		// Sidebar refresh deliberately collects only this pane's provider and leaves
		// Attach nil, avoiding the heavier cross-pane activity aggregation path.
		collectOptions.Only = map[string]bool{limitsProviderID: true}
		collected := limits.CollectAllProviderLimits(cwd, nowMs, collectOptions)
		if len(collected) > 0 {
			providerLimits = &collected[0]
		}
	}
	fallbackProviderText := formatSidebarProvider(*pane.Agent, p.AgentID(), snapshot)
	providerText, limitText := formatSidebarBillingTokens(
		billingMode, fallbackProviderText, displayProviderID,
		providerLimits, totalTokens, totalCostUSD, nowMs, limits.ResolvedLimitPercent(),
	)

	// With 2+ configured accounts of the same family, the $limit row's job
	// shifts from "show the limit" to "show which account this pane is"
	// since that's otherwise invisible in the sidebar. The limit percentage
	// moves down into $context instead.
	multiProfile := (*pane.Agent == "claude" && len(claudeProfiles) > 1) ||
		(*pane.Agent == "codex" && len(codexProfiles) > 1) ||
		(*pane.Agent == "grok" && len(grokProfiles) > 1) ||
		(*pane.Agent == "opencode" && len(openCodeProfiles) > 1)
	accountText := ""
	limitToken := limitText
	if multiProfile {
		switch *pane.Agent {
		case "claude":
			if profile, ok := findClaudeProfile(claudeProfiles, providerID); ok {
				accountText = resolveSidebarAccountLabel(profile)
			}
		case "codex":
			if profile, ok := findCodexProfile(codexProfiles, providerID); ok {
				accountText = profile.Label
			}
		case "grok":
			if profile, ok := findGrokProfile(grokProfiles, providerID); ok {
				accountText = profile.Label
			}
		case "opencode":
			if profile, ok := findOpenCodeProfile(openCodeProfiles, providerID); ok {
				accountText = profile.Label
			}
		}
		if accountText != "" {
			limitToken = accountText
		}
	}
	writeMetadataToken(pane.Tokens, paneID, "limit", limitToken, force, retainExistingOnEmpty)

	// Stands in for Herdr's `agent` token so a pay-as-you-go pane names the
	// backend it is actually billing ("deepseek") instead of the harness.
	writeMetadataToken(pane.Tokens, paneID, "provider", providerText, force, retainExistingOnEmpty)

	// Context tokens: claude is read from its resolved profile's own transcript
	// root (bypassing the registry's default-root lookup) so a non-default
	// account's context display doesn't fall back to ~/.claude/projects.
	var usage *core.ContextUsage
	switch *pane.Agent {
	case "claude":
		if profile, ok := findClaudeProfile(claudeProfiles, providerID); ok && sid != nil {
			if transcript := claude.ResolveUsageForSessionIn(profile.ProjectsRoot, *sid); transcript != nil {
				u := claude.ToContextUsage(*transcript)
				usage = &u
			}
		}
	case "codex":
		if profile, ok := findCodexProfile(codexProfiles, providerID); ok {
			if extracted := codex.ResolveUsageForCodexIn(profile.Home, sid, cwd); extracted != nil {
				u := core.ContextUsage{ContextTokens: extracted.ContextTokens}
				if extracted.WindowTokens != nil {
					u.WindowTokens = extracted.WindowTokens
				}
				usage = &u
			}
		}
	case "grok":
		if profile, ok := findGrokProfile(grokProfiles, providerID); ok {
			if profile.Implicit {
				usage = p.ResolveUsage(provider.UsageResolveInput{Session: pane.AgentSession, Cwd: cwd, PaneID: &paneID})
			} else {
				usage = grok.ResolveUsageForGrokIn(profile.Home, sid, cwd)
			}
		}
	case "opencode":
		if profile, ok := findOpenCodeProfile(openCodeProfiles, providerID); ok {
			if profile.Implicit {
				usage = p.ResolveUsage(provider.UsageResolveInput{Session: pane.AgentSession, Cwd: cwd, PaneID: &paneID})
			} else {
				usage = opencode.ResolveUsageForOpenCodeIn(profile.DataDir, sid, cwd)
			}
		}
	default:
		usage = p.ResolveUsage(provider.UsageResolveInput{
			Session: pane.AgentSession,
			Cwd:     cwd,
			PaneID:  &paneID,
		})
	}

	contextPrefix := ""
	if accountText != "" {
		contextPrefix = limitText
	}

	if usage == nil {
		writeMetadataToken(pane.Tokens, paneID, "context", contextPrefix, force, retainExistingOnEmpty)
		return
	}

	liveWidth := herdrcli.GetSidebarWidthColumns(paneID)
	sidebarW := core.ResolveSidebarWidth(liveWidth, core.ResolveConfigSidebarWidth())
	maxCols := core.EstimateStatusMaxColumns(&sidebarW)
	maxCols = reserveColumnsFor(maxCols, contextPrefix)
	statusText := core.FormatUsageStatus(*usage, core.FormatUsageOptions{MaxColumns: maxCols})
	writeMetadataToken(pane.Tokens, paneID, "context", combineLimitAndContext(contextPrefix, statusText), force, retainExistingOnEmpty)
}
