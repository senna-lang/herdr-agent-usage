/**
 * Verifies that live sidebar updates retain last-known-good metadata during
 * transient collection gaps, while settled and forced updates may clear it.
 */
package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUpdate_MetadataClearingPolicy(t *testing.T) {
	tests := []struct {
		name        string
		status      string
		force       bool
		shouldClear bool
	}{
		{name: "working update retains last good values", status: "working", shouldClear: false},
		{name: "settled update clears stale values", status: "idle", shouldClear: true},
		{name: "forced working update clears stale values", status: "working", force: true, shouldClear: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			logPath := filepath.Join(root, "metadata.log")
			binPath := filepath.Join(root, "fake-herdr")
			script := `#!/bin/sh
if [ "$1" = pane ] && [ "$2" = get ]; then
  printf '{"result":{"pane":{"agent":"omp","agent_status":"%s","label":"review-pane","cwd":"/tmp","tokens":{"limit":"last-good-limit","context":"last-good-context"}}}}\n' "$PANE_STATUS"
  exit 0
fi
if [ "$1" = pane ] && [ "$2" = report-metadata ]; then
  printf '%s\n' "$*" >> "$REVIEW_METADATA_LOG"
fi
`
			if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HERDR_PANE_ID", "test-pane")
			t.Setenv("HERDR_BIN_PATH", binPath)
			t.Setenv("PANE_STATUS", tt.status)
			t.Setenv("REVIEW_METADATA_LOG", logPath)
			t.Setenv("OMP_SESSIONS_ROOT", filepath.Join(root, "sessions"))
			t.Setenv("HOME", root)

			RunUpdate(tt.force)

			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("metadata calls = none: %v", err)
			}
			cleared := strings.Contains(string(data), "--clear-token limit") && strings.Contains(string(data), "--clear-token context")
			if cleared != tt.shouldClear {
				t.Fatalf("cleared=%t, want %t; metadata calls = %q", cleared, tt.shouldClear, data)
			}
		})
	}
}
