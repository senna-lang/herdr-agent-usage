/**
 * Tests for per-token content-based write deduplication.
 */
package core

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestMarkTokenWritten_RemovesLegacyStatusFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	legacy := filepath.Join(dir, "last-status-w1_p1.txt")
	if err := os.WriteFile(legacy, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	MarkTokenWritten("w1:p1", "context", "new")
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy state still exists: %v", err)
	}
}
