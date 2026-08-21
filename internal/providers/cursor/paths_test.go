/**
 * Tests for Cursor state-directory resolution.
 */
package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateDir_UsagebarOverrideWins(t *testing.T) {
	t.Setenv("USAGEBAR_STATE_DIR", "/explicit/state")
	t.Setenv("CURSOR_CONFIG_DIR", "/some/cursor")
	if got := StateDir(); got != "/explicit/state" {
		t.Fatalf("StateDir() = %q", got)
	}
}

func TestStateDir_FollowsCursorConfigDir(t *testing.T) {
	t.Setenv("USAGEBAR_STATE_DIR", "")
	t.Setenv("CURSOR_CONFIG_DIR", "/custom/cursor")
	if got := StateDir(); got != filepath.Join("/custom/cursor", stateDirName) {
		t.Fatalf("StateDir() = %q", got)
	}
}

func TestStateDir_DefaultsUnderHome(t *testing.T) {
	t.Setenv("USAGEBAR_STATE_DIR", "")
	t.Setenv("CURSOR_CONFIG_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := StateDir(); got != filepath.Join(home, ".cursor", stateDirName) {
		t.Fatalf("StateDir() = %q", got)
	}
}

func TestSessionsDir(t *testing.T) {
	if got := SessionsDir("/state"); got != filepath.Join("/state", sessionsDirName) {
		t.Fatalf("SessionsDir() = %q", got)
	}
}
