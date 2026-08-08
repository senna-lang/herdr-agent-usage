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

// combineLimitAndCompactContext joins a limit window with compact context
// tokens for a single-row sidebar meter: "7d 77%  ⛁ 94k". Two spaces separate
// the account window from context so the groups read apart without an icon.
// Kept for tests and any caller that still needs a plain combined string;
// live sidebar writes use exclusive $ctx_* tier tokens instead (Herdr strips
// ANSI, so color comes from per-token fg in config).
func combineLimitAndCompactContext(limitText, compactTokens string) string {
	if limitText == "" {
		return compactTokens
	}
	if compactTokens == "" {
		return limitText
	}
	return limitText + "  " + compactTokens
}

// writeContextTierTokens writes the exclusive $ctx / $ctx_y / $ctx_yy / $ctx_r
// map so only the matching absolute-size tier is non-empty.
//
// usage == nil clears every tier. Prefer blank over a sibling pane's count:
// Grok panes often share $HOME as cwd, and a failed/ambiguous resolve used to
// leave a stale 146k from another session while the UI showed 23k.
func writeContextTierTokens(current map[string]string, paneID string, usage *core.ContextUsage, force bool) {
	values := map[string]string{
		core.ContextTokenOK:       "",
		core.ContextTokenSoft:     "",
		core.ContextTokenWarn:     "",
		core.ContextTokenCritical: "",
	}
	if usage != nil {
		values = core.ContextTierTokenValues(*usage)
	}
	for _, name := range core.AllContextTierTokenNames {
		writeMetadataToken(current, paneID, name, values[name], force)
	}
}

// writeLimitToken writes $limit, but refuses to clear it on a transient
// subscription collect miss. Empty limitText only clears when we know the
// pane truly has no window (pay-as-you-go with no burn, or collected limits
// with no windows).
func writeLimitToken(
	current map[string]string,
	paneID, limitText string,
	billingMode limits.BillingMode,
	collected bool,
	force bool,
) {
	if limitText != "" {
		writeMetadataToken(current, paneID, "limit", limitText, force)
		return
	}
	if billingMode == limits.BillingPayAsYouGo {
		// No session burn → clear is correct.
		writeMetadataToken(current, paneID, "limit", "", force)
		return
	}
	if collected {
		// Provider responded with no usable windows.
		writeMetadataToken(current, paneID, "limit", "", force)
		return
	}
	// Subscription collect returned nothing — keep the previous $limit so a
	// flaky fetch cannot leave the row as "only red context".
	if force && current["limit"] == "" {
		// force + already empty: nothing to do
		return
	}
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
) (providerText, limitText string) {
	providerText = fallbackProviderText
	if billingMode != limits.BillingPayAsYouGo {
		if billingMode == limits.BillingSubscription {
			providerText = displayProviderID
		}
		if providerLimits != nil {
			limitText = limits.FormatSidebarLimit(*providerLimits, nowMs)
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

func writeMetadataToken(current map[string]string, paneID, name, value string, force bool) {
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

// RunUpdate resolves usage for HERDR_PANE_ID and updates its sidebar tokens.
//
// Always refreshes (including while status is "working"): Grok compaction and
// mid-turn context drops update signals.json before the agent settles, and
// skipping those events left the sidebar stuck on the pre-compress count.
// force=true still forces token writes even when values are unchanged.
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

	// Row 1 stands in for Herdr's built-in `tab`/`pane` tokens, which render
	// blank whenever a tab keeps its auto-assigned numeric label and the
	// pane was never renamed. Fall back through the workspace ("space")
	// name so the row is never empty.
	naming := herdrcli.GetPaneNaming(pane)
	title := core.ResolveSidebarTitle(naming.PaneLabel, naming.TabLabel, naming.TabNumber, naming.WorkspaceLabel)
	writeMetadataToken(pane.Tokens, paneID, "title", title, force)

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

	// Resolve which specific provider this pane belongs to. For claude this is
	// profile-aware (session-transcript match across configured accounts);
	// other agents resolve 1:1 with p.AgentID() as before. ok=false only
	// happens for an ambiguous multi-profile claude pane.
	claudeProfiles := limits.ResolvedClaudeProfiles()
	providerID, resolved := limits.BuildClaudePaneProviderResolver(claudeProfiles)(snapshot)
	if !resolved {
		// Cannot tell which account this pane belongs to: clear rather than
		// guess into the wrong account's limits/tokens.
		writeMetadataToken(pane.Tokens, paneID, "limit", "", force)
		writeMetadataToken(pane.Tokens, paneID, "window", "", force)
		writeMetadataToken(pane.Tokens, paneID, "provider", formatSidebarProvider(*pane.Agent, p.AgentID(), snapshot), force)
		writeContextTierTokens(pane.Tokens, paneID, nil, force)
		writeMetadataToken(pane.Tokens, paneID, "context", "", force)
		return
	}

	billingMode := limits.PaneBillingMode(providerID, snapshot, limits.DefaultBillingDeps())
	limitsProviderID := limits.SubscriptionLimitsProviderID(providerID, snapshot)
	displayProviderID := limits.SubscriptionDisplayProviderID(providerID, snapshot)
	var providerLimits *limits.ProviderLimits
	var totalTokens, totalCostUSD float64
	collectedLimits := false
	if billingMode == limits.BillingPayAsYouGo {
		totalTokens, totalCostUSD = limits.PaneTotalUsage(providerID, snapshot, nowMs)
		collectedLimits = true // pay-as-you-go path is authoritative even when burn is 0
	} else {
		collectOptions := limits.DefaultCollectOptions()
		// Sidebar refresh deliberately collects only this pane's provider and leaves
		// Attach nil, avoiding the heavier cross-pane activity aggregation path.
		collectOptions.Only = map[string]bool{limitsProviderID: true}
		collected := limits.CollectAllProviderLimits(cwd, nowMs, collectOptions)
		if len(collected) > 0 {
			providerLimits = &collected[0]
			collectedLimits = true
		}
	}
	fallbackProviderText := formatSidebarProvider(*pane.Agent, p.AgentID(), snapshot)
	providerText, limitText := formatSidebarBillingTokens(
		billingMode, fallbackProviderText, displayProviderID,
		providerLimits, totalTokens, totalCostUSD, nowMs,
	)

	// With 2+ configured Claude accounts, the $limit row's job shifts from
	// "show the limit" to "show which account this pane is" (joined with
	// $provider as "claude · you@example.com") since that's otherwise
	// invisible in the sidebar. The limit percentage moves into $window,
	// as this pane's own account is already unambiguous.
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

	// Stands in for Herdr's `agent` token so a pay-as-you-go pane names the
	// backend it is actually billing ("deepseek") instead of the harness.
	// Optional in sidebar rows — narrow agent panels usually omit $provider.
	writeMetadataToken(pane.Tokens, paneID, "provider", providerText, force)

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

	// Split account/limit window from the absolute context count so Herdr can
	// color only the count via per-token fg ($ctx / $ctx_y / $ctx_yy / $ctx_r).
	// Metadata values cannot carry ANSI (Herdr strips escapes).
	// Multi-profile Claude: $limit = account, $window = "5h 88%", $ctx_* = count.
	// Single-profile: $limit = window, $window cleared, $ctx_* = count.
	// Always clear legacy $context so old two-row layouts do not show stale text.
	if accountText != "" {
		// Account identity is always known once multiProfile resolved; write it.
		writeMetadataToken(pane.Tokens, paneID, "limit", limitToken, force)
		if limitText != "" {
			writeMetadataToken(pane.Tokens, paneID, "window", limitText, force)
		} else if collectedLimits {
			writeMetadataToken(pane.Tokens, paneID, "window", "", force)
		}
		// else: keep previous $window on collect miss
	} else {
		writeLimitToken(pane.Tokens, paneID, limitToken, billingMode, collectedLimits, force)
		writeMetadataToken(pane.Tokens, paneID, "window", "", force)
	}
	writeContextTierTokens(pane.Tokens, paneID, usage, force)
	writeMetadataToken(pane.Tokens, paneID, "context", "", force)
}
