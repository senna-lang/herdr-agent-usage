/**
 * Tests for server-state-based write deduplication.
 */
package core

import "testing"

func TestShouldWriteToken(t *testing.T) {
	current := map[string]string{"context": "⛁ 10% (10k)"}
	if ShouldWriteToken(current, "context", "⛁ 10% (10k)", false) {
		t.Fatal("matching server value should skip")
	}
	if !ShouldWriteToken(current, "context", "⛁ 21% (21k)", false) {
		t.Fatal("changed value should be allowed")
	}
	if !ShouldWriteToken(current, "context", "⛁ 10% (10k)", true) {
		t.Fatal("force should allow an identical value")
	}
	if !ShouldWriteToken(current, "context", "", false) {
		t.Fatal("clearing a present token should be allowed")
	}
}

func TestShouldWriteToken_AbsentKeyReadsAsEmpty(t *testing.T) {
	current := map[string]string{"context": "same"}
	if !ShouldWriteToken(current, "limit", "5h 72%", false) {
		t.Fatal("value for an absent token must be written")
	}
	if ShouldWriteToken(current, "limit", "", false) {
		t.Fatal("clearing an absent token is a no-op")
	}
}

func TestShouldWriteToken_NilMap(t *testing.T) {
	if !ShouldWriteToken(nil, "provider", "claude", false) {
		t.Fatal("nil map must not suppress a value write")
	}
	if ShouldWriteToken(nil, "provider", "", false) {
		t.Fatal("clearing against a nil map is a no-op")
	}
	if !ShouldWriteToken(nil, "provider", "", true) {
		t.Fatal("force should write even a clear against a nil map")
	}
}
