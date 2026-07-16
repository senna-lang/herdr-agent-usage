/**
 * Tests for Claude provider session-kind interpretation.
 */
package claude

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

func TestProvider_AgentID(t *testing.T) {
	if Provider.AgentID() != "claude" {
		t.Fatalf("got %q", Provider.AgentID())
	}
}

func TestProvider_NullCases(t *testing.T) {
	if Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "path", Value: "/tmp/x"},
	}) != nil {
		t.Fatal("expected nil for non-id kind")
	}
	if Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: ""},
	}) != nil {
		t.Fatal("expected nil for empty value")
	}
	if Provider.ResolveUsage(provider.UsageResolveInput{Session: nil}) != nil {
		t.Fatal("expected nil for no session")
	}
	if Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: "00000000-0000-0000-0000-000000000000"},
	}) != nil {
		t.Fatal("expected nil for unknown UUID")
	}
}
