/**
 * Antigravity CLI state-directory resolution.
 *
 * This location is shared between two processes that do not share an
 * environment: the statusLine bridge runs inside the `agy` process, while the
 * provider runs in a process Herdr spawns later. ANTIGRAVITY_CLI_CONFIG_DIR is
 * visible only to the writer, so deriving the path from it would file
 * snapshots where the reader never looks — the same reasoning Cursor's
 * provider documents for CURSOR_CONFIG_DIR.
 *
 * The default therefore depends on nothing but the home directory.
 * USAGEBAR_STATE_DIR stays available as the deliberate, user-global override
 * shared with every other provider's derived state.
 */
package antigravity

import (
	"os"
	"path/filepath"
)

// defaultConfigDirName is Antigravity CLI's default app-data directory under
// the home directory, confirmed from a real installation
// (`~/.gemini/antigravity-cli/`). Used only to anchor plugin state, never to
// locate Antigravity's own config.
const defaultConfigDirName = ".gemini/antigravity-cli"

// stateDirName is the plugin-owned subdirectory inside the agent's config
// dir, matching the convention used for every other agent's derived state.
const stateDirName = "herdr-usagebar"

// sessionsDirName holds one snapshot per Antigravity conversation id.
const sessionsDirName = "sessions"

// StateDir returns the directory holding Antigravity's plugin-derived state.
//
// Deliberately independent of ANTIGRAVITY_CLI_CONFIG_DIR: see the package
// comment.
func StateDir() string {
	if dir := os.Getenv("USAGEBAR_STATE_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(defaultConfigDirName), stateDirName)
}

// SessionsDir returns the directory holding per-session snapshots.
func SessionsDir(stateDir string) string {
	return filepath.Join(stateDir, sessionsDirName)
}
