/**
 * Tests for per-token content-based write deduplication.
 */
package core

import "testing"

func TestShouldWriteToken(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())

	if !ShouldWriteToken("w1:p1", "context", "⛁ 10% (10k)", false) {
		t.Fatal("first context write should be allowed")
	}
	MarkTokenWritten("w1:p1", "context", "⛁ 10% (10k)")
	if ShouldWriteToken("w1:p1", "context", "⛁ 10% (10k)", false) {
		t.Fatal("identical context should skip")
	}
	if !ShouldWriteToken("w1:p1", "context", "⛁ 21% (21k)", false) {
		t.Fatal("changed context should be allowed")
	}
	if !ShouldWriteToken("w1:p1", "context", "⛁ 10% (10k)", true) {
		t.Fatal("force should allow an identical value")
	}
}

func TestShouldWriteToken_TracksNamesAndClearsIndependently(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())

	MarkTokenWritten("w1:p1", "context", "same")
	if !ShouldWriteToken("w1:p1", "limit", "same", false) {
		t.Fatal("different token names must not share state")
	}

	if !ShouldWriteToken("w1:p1", "limit", "", false) {
		t.Fatal("first clear should be reported")
	}
	MarkTokenWritten("w1:p1", "limit", "")
	if ShouldWriteToken("w1:p1", "limit", "", false) {
		t.Fatal("repeated clear should skip")
	}
	if ShouldWriteToken("w1:p1", "context", "same", false) {
		t.Fatal("limit clear must not disturb context state")
	}
}
