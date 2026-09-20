/**
 * Verifies that a stale HERDR_BIN_PATH cannot silently disable sidebar
 * metadata writes: when the running binary was replaced or removed on disk
 * (the kernel reports it as `<path> (deleted)`, and a package manager that
 * rotates version directories leaves the same kind of dead path behind), the
 * plugin must still reach herdr through PATH instead of failing every callback
 * with ENOENT.
 */
package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUpdate_StaleHerdrBinPathStillWritesTokens(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "metadata.log")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = pane ] && [ "$2" = get ]; then
  printf '{"result":{"pane":{"agent":"omp","agent_status":"idle","label":"review-pane","cwd":"/tmp"}}}\n'
  exit 0
fi
if [ "$1" = pane ] && [ "$2" = report-metadata ]; then
  printf '%s\n' "$*" >> "$REVIEW_METADATA_LOG"
fi
`
	if err := os.WriteFile(filepath.Join(binDir, "herdr"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	// The path Herdr exports can outlive the binary it points at: a live
	// handoff from a version directory that the package manager later removed
	// resolves to a deleted file.
	t.Setenv("HERDR_BIN_PATH", filepath.Join(root, "0.8.2", "herdr"))
	t.Setenv("HERDR_PANE_ID", "test-pane")
	t.Setenv("REVIEW_METADATA_LOG", logPath)
	t.Setenv("OMP_SESSIONS_ROOT", filepath.Join(root, "sessions"))
	t.Setenv("HOME", root)

	RunUpdate(false)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("metadata calls = none: %v", err)
	}
	if !strings.Contains(string(data), "--token title=review-pane") {
		t.Fatalf("metadata calls = %q", data)
	}
}
