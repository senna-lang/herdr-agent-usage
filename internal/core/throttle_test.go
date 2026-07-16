/**
 * Tests for the content-based write-skip decision.
 */
package core

import (
	"testing"
)

func TestShouldWriteStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)

	if !ShouldWriteStatus("w1:p1", "⛁ 10% (10k)", false) {
		t.Fatal("first write should be allowed")
	}
	MarkStatusWritten("w1:p1", "⛁ 10% (10k)")
	if ShouldWriteStatus("w1:p1", "⛁ 10% (10k)", false) {
		t.Fatal("identical should skip")
	}
	if !ShouldWriteStatus("w1:p1", "⛁ 21% (213k)", false) {
		t.Fatal("changed should allow")
	}
	if !ShouldWriteStatus("w1:p1", "⛁ 10% (10k)", true) {
		t.Fatal("force should allow")
	}
}

func TestClearTracking(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)

	MarkStatusCleared("w1:p1")
	if !IsAlreadyCleared("w1:p1") {
		t.Fatal("expected cleared")
	}
	if ShouldWriteStatus("w1:p1", "", false) {
		t.Fatal("re-clear should skip")
	}
}
