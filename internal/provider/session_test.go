/**
 * Tests for SessionID extraction.
 */
package provider

import "testing"

func TestSessionID_IDKind(t *testing.T) {
	got := SessionID(UsageResolveInput{Session: &AgentSession{Kind: "id", Value: "abc-123"}})
	if got == nil || *got != "abc-123" {
		t.Fatalf("got %v, want abc-123", got)
	}
}

func TestSessionID_NilSession(t *testing.T) {
	if got := SessionID(UsageResolveInput{}); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestSessionID_NonIDKind(t *testing.T) {
	if got := SessionID(UsageResolveInput{Session: &AgentSession{Kind: "path", Value: "/x"}}); got != nil {
		t.Fatalf("got %v, want nil for kind=path", got)
	}
}

func TestSessionID_EmptyValue(t *testing.T) {
	if got := SessionID(UsageResolveInput{Session: &AgentSession{Kind: "id", Value: ""}}); got != nil {
		t.Fatalf("got %v, want nil for empty value", got)
	}
}
