/**
 * Tests for FindProvider.
 */
package providers

import "testing"

func TestFindProvider_Registered(t *testing.T) {
	for _, id := range []string{"claude", "codex", "grok", "opencode"} {
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
