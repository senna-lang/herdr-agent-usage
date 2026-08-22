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

// Plugin state must not follow Cursor's config directory: only the writer can
// see that setting, so following it would split writer and reader apart.
func TestStateDir_DoesNotFollowCursorConfigDir(t *testing.T) {
	t.Setenv("USAGEBAR_STATE_DIR", "")
	t.Setenv("CURSOR_CONFIG_DIR", "/custom/cursor")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := StateDir(); got != filepath.Join(home, defaultConfigDirName, stateDirName) {
		t.Fatalf("StateDir() = %q, want the home-anchored default", got)
	}
}

func TestStateDir_DefaultsUnderHome(t *testing.T) {
	t.Setenv("USAGEBAR_STATE_DIR", "")
	t.Setenv("CURSOR_CONFIG_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := StateDir(); got != filepath.Join(home, defaultConfigDirName, stateDirName) {
		t.Fatalf("StateDir() = %q", got)
	}
}

func TestSessionsDir(t *testing.T) {
	if got := SessionsDir("/state"); got != filepath.Join("/state", sessionsDirName) {
		t.Fatalf("SessionsDir() = %q", got)
	}
}

// The statusLine writer runs inside the Cursor process and inherits whatever
// CURSOR_CONFIG_DIR that process was launched with. The provider runs later in
// a process herdr spawns, which does not inherit it. Deriving the state
// location from that variable therefore writes snapshots where the reader never
// looks, and the sidebar stays empty for the entire supported custom-config
// layout. The shared location must not depend on the writer's environment.
func TestStateDir_IsIndependentOfCursorProcessEnvironment(t *testing.T) {
	t.Setenv("USAGEBAR_STATE_DIR", "")

	t.Setenv("CURSOR_CONFIG_DIR", "/custom/cursor")
	writerView := StateDir()

	t.Setenv("CURSOR_CONFIG_DIR", "")
	readerView := StateDir()

	if writerView != readerView {
		t.Fatalf("writer resolved %q but reader resolved %q; snapshots would be unreadable", writerView, readerView)
	}
}
