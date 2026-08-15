/**
 * Exhaustiveness contract between providers.Registrations and the
 * internal/limits dispatch tables that depend on it.
 *
 * Each test below asserts bidirectional equality (declared set == table's
 * key set) so both a missing addition and a stale leftover are caught. See
 * AGENTS.md's "Exhaustive Constraints for Evolving Definitions": a provider
 * newly declaring a capability must fail one of these tests until every
 * dependent table is updated, instead of compiling clean and silently never
 * appearing in the panel.
 */
package limits

import (
	"sort"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/providers"
	claudeprovider "github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
)

func sortedCopy(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func assertSameIDSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	gotSorted, wantSorted := sortedCopy(got), sortedCopy(want)
	if len(gotSorted) != len(wantSorted) {
		t.Fatalf("%s: got %v, want %v", label, gotSorted, wantSorted)
	}
	for i := range gotSorted {
		if gotSorted[i] != wantSorted[i] {
			t.Fatalf("%s: got %v, want %v", label, gotSorted, wantSorted)
		}
	}
}

// TestAgentToProvider_MatchesRegistrations guards attachactivity.go's derived
// map: every registered provider must resolve pane activity by its own id.
func TestAgentToProvider_MatchesRegistrations(t *testing.T) {
	var want []string
	for _, p := range providers.All {
		want = append(want, p.AgentID())
	}
	var got []string
	for agentID, providerID := range agentToProvider {
		if agentID != providerID {
			t.Errorf("agentToProvider[%q] = %q, want identity", agentID, providerID)
		}
		got = append(got, agentID)
	}
	assertSameIDSet(t, "agentToProvider keys", got, want)
}

// TestNonClaudeQuotaOwnerIDs_MatchCapabilityRegistrations guards
// billingmode.go's derived list: every non-Claude quota-owning provider must
// be present, and no other provider must be.
func TestNonClaudeQuotaOwnerIDs_MatchCapabilityRegistrations(t *testing.T) {
	claudeID := claudeprovider.Provider.AgentID()
	var want []string
	for _, id := range providers.IDsWithCapability(providers.CapOwnsSubscriptionQuota) {
		if id != claudeID {
			want = append(want, id)
		}
	}
	assertSameIDSet(t, "nonClaudeProviderIDs", nonClaudeProviderIDs, want)
}

// TestNonClaudeQuotaSpecs_MatchCapabilityRegistrations guards collect.go's
// CollectAllProviderLimits wiring: every non-Claude quota-owning provider
// must have a collect spec, and no other provider must have one.
func TestNonClaudeQuotaSpecs_MatchCapabilityRegistrations(t *testing.T) {
	claudeID := claudeprovider.Provider.AgentID()
	var want []string
	for _, id := range providers.IDsWithCapability(providers.CapOwnsSubscriptionQuota) {
		if id != claudeID {
			want = append(want, id)
		}
	}
	var got []string
	for _, s := range nonClaudeQuotaSpecs {
		got = append(got, s.id)
	}
	assertSameIDSet(t, "nonClaudeQuotaSpecs ids", got, want)
}

// TestLimitIDSlotTables_MatchCapabilityRegistrations guards windowpool.go's
// per-provider limit-id vocabulary: every quota-owning provider (including
// Claude) must have a table, and no other provider must have one.
func TestLimitIDSlotTables_MatchCapabilityRegistrations(t *testing.T) {
	want := providers.IDsWithCapability(providers.CapOwnsSubscriptionQuota)
	var got []string
	for id := range limitIDSlotTables {
		got = append(got, id)
	}
	assertSameIDSet(t, "limitIDSlotTables keys", got, want)
}
