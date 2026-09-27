//go:build !windows

package update

import (
	"os"
	"testing"
)

// writeHerdrShim installs script as an executable herdr stand-in at path and
// returns the path to put in HERDR_BIN_PATH, or whose directory to put on PATH.
func writeHerdrShim(t *testing.T, path, script string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
