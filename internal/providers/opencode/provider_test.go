/**
 * Boundary tests for opencodeProvider.
 */
package opencode

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

func TestProvider_AgentID(t *testing.T) {
	if Provider.AgentID() != "opencode" {
		t.Fatalf("got %q", Provider.AgentID())
	}
}

func TestProvider_MissingDB(t *testing.T) {
	t.Setenv("OPENCODE_DB", "/tmp/definitely-missing-opencode.db")
	if Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: "ses_x"},
	}) != nil {
		t.Fatal("expected nil")
	}
}
