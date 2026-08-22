/**
 * Cursor state-directory resolution.
 *
 * This location is shared between two processes that do not share an
 * environment: the statusLine bridge runs inside the Cursor process, while the
 * provider runs in a process Herdr spawns later. Anything Cursor was launched
 * with — CURSOR_CONFIG_DIR in particular — is visible only to the writer, so
 * deriving the path from it would file snapshots where the reader never looks.
 *
 * The default therefore depends on nothing but the home directory, matching how
 * notify state resolves for Claude (internal/ratelimit) and for the same
 * reason. USAGEBAR_STATE_DIR stays available as the deliberate, user-global
 * override that both processes can be given.
 *
 * Cursor's own config may still live elsewhere; setup reports the documented
 * locations for that, but plugin-derived state does not follow it.
 */
package cursor

import (
	"os"
	"path/filepath"
)

// stateDirName is the plugin-owned subdirectory inside the agent's config dir,
// matching the convention used for every other agent's derived state.
// defaultConfigDirName is Cursor's default config directory under the home
// directory. Used only to anchor plugin state, never to locate Cursor's config.
const defaultConfigDirName = ".cursor"

const stateDirName = "herdr-usagebar"

// sessionsDirName holds one snapshot per Cursor session id.
const sessionsDirName = "sessions"

// StateDir returns the directory holding Cursor's plugin-derived state.
//
// Deliberately independent of CURSOR_CONFIG_DIR: see the package comment.
func StateDir() string {
	if dir := os.Getenv("USAGEBAR_STATE_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, defaultConfigDirName, stateDirName)
}

// SessionsDir returns the directory holding per-session snapshots.
func SessionsDir(stateDir string) string {
	return filepath.Join(stateDir, sessionsDirName)
}
