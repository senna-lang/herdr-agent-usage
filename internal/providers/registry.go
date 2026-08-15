/**
 * UsageProvider registry for the supported agents.
 * To add a new agent, register it here and add a providers/<agent>/ directory.
 *
 * Every provider also declares its limits/billing capabilities, so dependent
 * dispatch tables in internal/limits can be checked against this one list
 * instead of duplicating the provider set. See Capability for the meaning of
 * each flag and registry_test.go for the exhaustiveness contract.
 */
package providers

import (
	"slices"

	"github.com/senna-lang/herdr-agent-usage/internal/provider"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/omp"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
)

// Capability describes a limits/billing-relevant trait of a provider. This is
// a separate axis from UsageProvider (which is about context-usage
// resolution): every registered provider participates in context usage and
// pane-activity attribution identically, but not every provider owns its own
// rate-limit quota.
type Capability int

const (
	// CapOwnsSubscriptionQuota means the provider's own on-disk state directly
	// records subscription rate-limit windows, so internal/limits runs a
	// collector for it (see CollectAllProviderLimits's per-provider specs and
	// windowpool.go's slotForLimitID).
	CapOwnsSubscriptionQuota Capability = iota
	// CapRoutesToCollector means the provider never records its own quota.
	// Its session data may report a backend id that maps to another
	// provider's collector instead (see SubscriptionRouteForProviderAuth in
	// internal/limits). Currently OMP and Pi: both can run through an
	// OpenCode Go or Grok login without owning either quota themselves.
	CapRoutesToCollector
)

// Registration pairs one provider with its declared capabilities.
type Registration struct {
	Provider     provider.UsageProvider
	Capabilities []Capability
}

// Has reports whether this registration declares cap.
func (r Registration) Has(cap Capability) bool {
	return slices.Contains(r.Capabilities, cap)
}

// Registrations is the canonical source of truth for every supported agent
// and what it does at the limits/billing layer. Adding a provider here (with
// no capabilities) is caught by TestRegistrations_EveryProviderHasACapability;
// declaring CapOwnsSubscriptionQuota or CapRoutesToCollector without wiring
// the matching internal/limits table is caught by that table's own
// exhaustiveness test (see internal/limits/provider_contract_test.go).
var Registrations = []Registration{
	{claude.Provider, []Capability{CapOwnsSubscriptionQuota}},
	{codex.Provider, []Capability{CapOwnsSubscriptionQuota}},
	{grok.Provider, []Capability{CapOwnsSubscriptionQuota}},
	{omp.Provider, []Capability{CapRoutesToCollector}},
	{omp.PiProvider, []Capability{CapRoutesToCollector}},
	{opencode.Provider, []Capability{CapOwnsSubscriptionQuota}},
}

// All registered providers.
var All = allProviders()

func allProviders() []provider.UsageProvider {
	out := make([]provider.UsageProvider, len(Registrations))
	for i, r := range Registrations {
		out[i] = r.Provider
	}
	return out
}

// FindProvider returns the provider for agentId, or nil when unregistered.
func FindProvider(agentID string) provider.UsageProvider {
	for _, p := range All {
		if p.AgentID() == agentID {
			return p
		}
	}
	return nil
}

// IDsWithCapability returns the AgentID of every registered provider
// declaring cap, in registration order.
func IDsWithCapability(cap Capability) []string {
	var ids []string
	for _, r := range Registrations {
		if r.Has(cap) {
			ids = append(ids, r.Provider.AgentID())
		}
	}
	return ids
}
