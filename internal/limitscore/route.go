/**
 * Maps a harness session's recorded backend/provider id to the subscription
 * collector that owns its quota, when any does.
 *
 * Lives here (not internal/limits) because the window pool's
 * AccountWindowsFromOMP needs it to classify OMP-observed rows, and the pool
 * itself must be reachable from provider adapters without an import cycle.
 */
package limitscore

import "strings"

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
