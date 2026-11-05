/**
 * Tests for the Devin UsageProvider entry point.
 *
 * Herdr reports a Devin pane's agent_session as kind "id" carrying the
 * session's own name, so resolution is an exact lookup: a missing id, a
 * foreign kind, or an absent store must all yield no usage rather than a
 * guess at another session.
 */
package devin

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

func TestProviderResolveUsage(t *testing.T) {
	dir := writeStore(t, []storeSession{{
		id:              "bejeweled-whitefish",
		lastActivitySec: 100,
		nodes:           []storeNode{requestNode("req-1", 100, oneRequest)},
	}})
	t.Setenv("DEVIN_DATA_DIR", dir)

	usage := Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: "bejeweled-whitefish"},
	})
	if usage == nil {
		t.Fatal("expected usage for the pane's session")
	}
	if usage.ContextTokens != 1000 {
		t.Fatalf("context = %d", usage.ContextTokens)
	}
}

func TestProviderResolveUsage_RefusesNonIDSession(t *testing.T) {
	dir := writeStore(t, []storeSession{{
		id:              "bejeweled-whitefish",
		lastActivitySec: 100,
		nodes:           []storeNode{requestNode("req-1", 100, oneRequest)},
	}})
	t.Setenv("DEVIN_DATA_DIR", dir)

	for _, input := range []provider.UsageResolveInput{
		{},
		{Session: &provider.AgentSession{Kind: "path", Value: "bejeweled-whitefish"}},
		{Session: &provider.AgentSession{Kind: "id", Value: ""}},
		{Session: &provider.AgentSession{Kind: "id", Value: "other-session"}},
	} {
		if got := Provider.ResolveUsage(input); got != nil {
			t.Fatalf("input %+v should yield no usage, got %+v", input, got)
		}
	}
}

func TestProviderResolveUsage_NoStore(t *testing.T) {
	t.Setenv("DEVIN_DATA_DIR", t.TempDir())
	usage := Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: "anything"},
	})
	if usage != nil {
		t.Fatalf("absent store should yield no usage, got %+v", usage)
	}
}

// The adapter owns Devin's limit-id vocabulary: the shared layer consumes
// the normalized slot and duration, never the id strings.
func TestLimitWindowSlot(t *testing.T) {
	for _, c := range []struct {
		limitID string
		slot    provider.LimitWindowSlot
		minutes int
	}{
		{"devin:daily", provider.LimitWindowPrimary, 1440},
		{"devin:weekly", provider.LimitWindowSecondary, 10080},
		{"devin:unknown", provider.LimitWindowNone, 0},
		{"", provider.LimitWindowNone, 0},
	} {
		slot, minutes := Provider.LimitWindowSlot(c.limitID)
		if slot != c.slot || minutes != c.minutes {
			t.Errorf("LimitWindowSlot(%q) = (%v, %d), want (%v, %d)", c.limitID, slot, minutes, c.slot, c.minutes)
		}
	}
}
