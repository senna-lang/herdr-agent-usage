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

// TestRegistrations_QuotaAndRoutingArePartition asserts CapOwnsSubscriptionQuota
// and CapRoutesToCollector classify every registered provider exactly once
// between them, with no provider left unclassified or double-classified. A
// newly registered provider that declares neither (or both) fails here
// instead of silently missing from every dependent internal/limits table.
func TestRegistrations_QuotaAndRoutingArePartition(t *testing.T) {
	for _, r := range Registrations {
		ownsQuota := r.Has(CapOwnsSubscriptionQuota)
		routes := r.Has(CapRoutesToCollector)
		if ownsQuota == routes {
			t.Errorf("%s: must declare exactly one of CapOwnsSubscriptionQuota/CapRoutesToCollector, got owns=%v routes=%v", r.Provider.AgentID(), ownsQuota, routes)
		}
	}
}
