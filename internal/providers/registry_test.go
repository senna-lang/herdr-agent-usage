/**
 * Tests for FindProvider and the Registrations capability contract.
 */
package providers

import "testing"

func TestFindProvider_Registered(t *testing.T) {
	for _, id := range []string{"claude", "codex", "grok", "omp", "pi", "opencode"} {
		p := FindProvider(id)
		if p == nil || p.AgentID() != id {
			t.Fatalf("%s: got %#v", id, p)
		}
	}
}

func TestFindProvider_Unknown(t *testing.T) {
	if FindProvider("unknown-agent") != nil {
		t.Fatal("expected nil")
	}
}

// TestRegistrations_EveryProviderHasACapability guards against a provider
// being registered with an empty Capabilities slice, which would silently
// exclude it from every internal/limits table that derives from
// IDsWithCapability instead of failing loud.
func TestRegistrations_EveryProviderHasACapability(t *testing.T) {
	for _, r := range Registrations {
		if len(r.Capabilities) == 0 {
			t.Errorf("%s: registered with no declared capabilities", r.Provider.AgentID())
		}
	}
}

// TestRegistrations_CapabilitiesArePartition asserts the capability set
// classifies every registered provider exactly once: a provider either owns
// subscription quota, routes to another provider's collector, or reports
// context only. A newly registered provider that declares none (or more than
// one) fails here instead of silently missing from, or wrongly appearing in,
// every dependent internal/limits table.
func TestRegistrations_CapabilitiesArePartition(t *testing.T) {
	partition := []Capability{CapOwnsSubscriptionQuota, CapRoutesToCollector, CapContextOnly}
	for _, r := range Registrations {
		declared := 0
		for _, cap := range partition {
			if r.Has(cap) {
				declared++
			}
		}
		if declared != 1 {
			t.Errorf("%s: must declare exactly one partition capability, declared %d", r.Provider.AgentID(), declared)
		}
	}
}
