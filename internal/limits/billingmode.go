/**
 * Billing-mode detection: is a harness pane billed against a subscription
 * plan (rate-limit windows meaningful) or a pay-as-you-go API backend
 * (no subscription limits — hide them)?
 *
 * Pure detectors only; file/DB reads live in billingmode_io.go.
 *
 * Evidence per provider:
 *   claude   — account: ~/.claude.json cachedUsageUtilization /
 *              oauthAccount.billingType; session: deployment env flags
 *              (CLAUDE_CODE_USE_BEDROCK / VERTEX / FOUNDRY) or
 *              ANTHROPIC_*_BASE_URL from settings / process env
 *   codex    — pane rollout: token_count with rate_limits => subscription;
 *              token_count without rate_limits => API backend
 *   opencode — session's latest assistant message providerID ("opencode-go"
 *              is the subscription gateway; anything else is direct API)
 *   grok     — account: auth.json auth_mode (oidc/oauth/sso => subscription);
 *              session: custom model base_url via config.toml (non-xAI => PAYG)
 */
package limits

import (
	"encoding/json"
	"strings"
)

// BillingMode classifies how a pane/account is billed.
type BillingMode int

const (
	// BillingUnknown means no evidence either way — callers fail open (show).
	BillingUnknown BillingMode = iota
	// BillingSubscription means positive subscription evidence (show limits).
	BillingSubscription
	// BillingPayAsYouGo means positive API-billing evidence (hide limits).
	BillingPayAsYouGo
)

// nonClaudeProviderIDs is the fixed non-Claude portion of the display-order
// provider universe (matches collect.go). Claude's portion is dynamic — see
// BillingDeps.ClaudeProfileIDs — since a multi-profile setup's billing gate
// must consider each configured account independently.
var nonClaudeProviderIDs = []string{"codex", "opencode", "grok"}

// CombineBillingModes merges account- and session-level evidence.
// PayAsYouGo wins (positive evidence to hide), then Subscription.
func CombineBillingModes(a, b BillingMode) BillingMode {
	if a == BillingPayAsYouGo || b == BillingPayAsYouGo {
		return BillingPayAsYouGo
	}
	if a == BillingSubscription || b == BillingSubscription {
		return BillingSubscription
	}
	return BillingUnknown
}

// OpenCodeBillingModeFromProviderID maps a session's providerID to a mode.
// "opencode-go" is the subscription gateway; any other recorded backend
// (deepseek, ollama, anthropic, …) bills pay-as-you-go.
func OpenCodeBillingModeFromProviderID(providerID *string) BillingMode {
	if providerID == nil || *providerID == "" {
		return BillingUnknown
	}
	if *providerID == "opencode-go" {
		return BillingSubscription
	}
	return BillingPayAsYouGo
}

// SubscriptionRoute identifies the quota collector and sidebar label for a
// subscription gateway used inside another harness.  The harness and the
// subscription are deliberately separate: OMP/Pi may execute through an
// OpenCode Go or Grok login without themselves owning either quota.
type SubscriptionRoute struct {
	CollectorProviderID string
	DisplayProviderID   string
}

// OMPPiSubscriptionRoute maps the provider id recorded in an OMP/Pi session
// to one of the subscription collectors this plugin already implements.
//
// Keep this positive-evidence-only: an ordinary provider id such as
// "anthropic" may mean an API key as well as an OAuth login, and must not be
// guessed into a subscription account.
func OMPPiSubscriptionRoute(backendID string) (SubscriptionRoute, bool) {
	return SubscriptionRouteForProviderAuth(backendID, "")
}

// SubscriptionRouteForProviderAuth maps a session provider plus its recorded
// credential kind to one of this plugin's subscription collectors.  OAuth is
// required for ambiguous provider ids such as "anthropic"; the same id with
// an API key remains pay-as-you-go.
func SubscriptionRouteForProviderAuth(backendID, credentialType string) (SubscriptionRoute, bool) {
	backendID = strings.ToLower(strings.TrimSpace(backendID))
	credentialType = strings.ToLower(strings.TrimSpace(credentialType))
	switch strings.ToLower(strings.TrimSpace(backendID)) {
	case "opencode-go":
		return SubscriptionRoute{CollectorProviderID: "opencode", DisplayProviderID: "opencode-go"}, true
	case "xai-oauth":
		return SubscriptionRoute{CollectorProviderID: "grok", DisplayProviderID: "grok"}, true
	case "anthropic":
		if strings.Contains(credentialType, "oauth") {
			return SubscriptionRoute{CollectorProviderID: "claude", DisplayProviderID: "claude"}, true
		}
	case "openai", "openai-codex":
		if strings.Contains(credentialType, "oauth") {
			return SubscriptionRoute{CollectorProviderID: "codex", DisplayProviderID: "codex"}, true
		}
	case "openai-codex-oauth":
		return SubscriptionRoute{CollectorProviderID: "codex", DisplayProviderID: "codex"}, true
	default:
		return SubscriptionRoute{}, false
	}
	return SubscriptionRoute{}, false
}

// CodexBillingModeFromLines inspects a rollout tail. A token_count event
// carrying rate_limits proves a subscription backend (unless plan_type says
// API key); token_count events without any rate_limits mean the backend
// never reported windows — a custom base_url / API key session.
func CodexBillingModeFromLines(lines []string) BillingMode {
	sawTokenCount := false
	for i := len(lines) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(lines[i])
		if raw == "" {
			continue
		}
		var parsed struct {
			Type    string `json:"type"`
			Payload *struct {
				Type       string `json:"type"`
				RateLimits *struct {
					PlanType *string `json:"plan_type"`
				} `json:"rate_limits"`
				Info *struct {
					RateLimits *struct {
						PlanType *string `json:"plan_type"`
					} `json:"rate_limits"`
				} `json:"info"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if parsed.Type != "event_msg" || parsed.Payload == nil || parsed.Payload.Type != "token_count" {
			continue
		}
		sawTokenCount = true
		rate := parsed.Payload.RateLimits
		if rate == nil && parsed.Payload.Info != nil {
			rate = parsed.Payload.Info.RateLimits
		}
		if rate == nil {
			continue
		}
		if rate.PlanType != nil && strings.Contains(strings.ToLower(*rate.PlanType), "api") {
			return BillingPayAsYouGo
		}
		return BillingSubscription
	}
	if sawTokenCount {
		return BillingPayAsYouGo
	}
	return BillingUnknown
}

// ClaudeBillingModeFromJSON reads ~/.claude.json evidence. Subscription usage
// utilization is only cached for subscription accounts; a parseable config
// with neither utilization nor a subscription billingType is an API-key /
// Bedrock / Vertex setup.
func ClaudeBillingModeFromJSON(rawJSON string) BillingMode {
	var parsed struct {
		CachedUsageUtilization *struct {
			Utilization *json.RawMessage `json:"utilization"`
		} `json:"cachedUsageUtilization"`
		OAuthAccount *struct {
			BillingType *string `json:"billingType"`
		} `json:"oauthAccount"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &parsed); err != nil {
		return BillingUnknown
	}
	if parsed.CachedUsageUtilization != nil && parsed.CachedUsageUtilization.Utilization != nil {
		return BillingSubscription
	}
	if parsed.OAuthAccount != nil && parsed.OAuthAccount.BillingType != nil {
		if strings.Contains(strings.ToLower(*parsed.OAuthAccount.BillingType), "subscription") {
			return BillingSubscription
		}
		return BillingPayAsYouGo
	}
	return BillingPayAsYouGo
}

// GrokBillingModeFromAuthMode maps auth.json auth_mode to a mode.
// OAuth-style logins (oidc/oauth/sso) carry SuperGrok subscription credits;
// an API-key login bills per token.
func GrokBillingModeFromAuthMode(authMode *string) BillingMode {
	if authMode == nil || *authMode == "" {
		return BillingUnknown
	}
	m := strings.ToLower(*authMode)
	switch {
	case strings.Contains(m, "oidc") || strings.Contains(m, "oauth") || strings.Contains(m, "sso"):
		return BillingSubscription
	case strings.Contains(m, "api"):
		return BillingPayAsYouGo
	}
	return BillingUnknown
}

// BillingDeps injects billing-mode resolvers (for tests and I/O adapters).
type BillingDeps struct {
	// PaneMode resolves session-scoped evidence for one open pane.
	PaneMode func(providerID string, pane OpenPaneSnapshot) BillingMode
	// AccountMode resolves account-scoped evidence for a provider.
	AccountMode func(providerID string) BillingMode
	// ClaudeProfileIDs are the configured Claude profile ids, replacing the
	// single literal "claude" entry in BillingProviderFilter's provider
	// universe so each configured account is gated independently. Empty
	// defaults to ["claude"] (today's single-profile behavior).
	ClaudeProfileIDs []string
	// ResolvePane maps one harness pane to its billed provider while retaining
	// the harness id needed to read session-specific evidence.
	ResolvePane func(pane OpenPaneSnapshot) (providerID, harnessID string, ok bool)
}

// PaneBillingMode combines account- and session-level evidence for one pane.
func PaneBillingMode(providerID string, pane OpenPaneSnapshot, deps BillingDeps) BillingMode {
	account := BillingUnknown
	if deps.AccountMode != nil {
		account = deps.AccountMode(providerID)
	}
	session := BillingUnknown
	if deps.PaneMode != nil {
		session = deps.PaneMode(providerID, pane)
	}
	return CombineBillingModes(account, session)
}

// BillingProviderFilter returns the provider ids whose subscription limits
// may be displayed. A provider is excluded when its account bills
// pay-as-you-go, or when every open pane for it runs a pay-as-you-go
// backend. Providers without open panes (or when the pane query failed)
// are gated by account evidence alone — fail-open on Unknown.
func BillingProviderFilter(openPanes []OpenPaneSnapshot, paneQueryOK bool, deps BillingDeps) map[string]bool {
	claudeIDs := deps.ClaudeProfileIDs
	if len(claudeIDs) == 0 {
		claudeIDs = []string{"claude"}
	}
	allIDs := make([]string, 0, len(claudeIDs)+len(nonClaudeProviderIDs))
	allIDs = append(allIDs, claudeIDs...)
	allIDs = append(allIDs, nonClaudeProviderIDs...)

	type billedPane struct {
		harnessID string
		pane      OpenPaneSnapshot
	}
	byProvider := make(map[string][]billedPane)
	if paneQueryOK {
		for _, pane := range openPanes {
			providerID, harnessID, ok := "", "", false
			if deps.ResolvePane != nil {
				providerID, harnessID, ok = deps.ResolvePane(pane)
			} else if id, found := agentToProvider[strings.ToLower(pane.Agent)]; found {
				providerID, harnessID, ok = id, id, true
			}
			if ok {
				byProvider[providerID] = append(byProvider[providerID], billedPane{harnessID: harnessID, pane: pane})
			}
		}
	}
	set := make(map[string]bool)
	for _, providerID := range allIDs {
		account := BillingUnknown
		if deps.AccountMode != nil {
			account = deps.AccountMode(providerID)
		}
		if account == BillingPayAsYouGo {
			continue
		}
		panes := byProvider[providerID]
		if len(panes) == 0 {
			set[providerID] = true
			continue
		}
		for _, entry := range panes {
			session := BillingUnknown
			if deps.PaneMode != nil {
				session = deps.PaneMode(entry.harnessID, entry.pane)
			}
			if CombineBillingModes(account, session) != BillingPayAsYouGo {
				set[providerID] = true
				break
			}
		}
	}
	return set
}

// IntersectFilters intersects two Only-filters; nil means unrestricted.
func IntersectFilters(a, b map[string]bool) map[string]bool {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := make(map[string]bool)
	for id := range a {
		if b[id] {
			out[id] = true
		}
	}
	return out
}
