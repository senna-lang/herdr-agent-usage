/**
 * Cursor state-directory resolution.
 *
 * Cursor's CLI configuration reference documents two overrides for the config
 * directory: CURSOR_CONFIG_DIR (an explicit path) and, on Linux/BSD,
 * XDG_CONFIG_HOME/cursor. Their relative precedence is not documented, so this
 * package deliberately does not infer one: it honours the explicit
 * CURSOR_CONFIG_DIR and otherwise uses the documented ~/.cursor default.
 * USAGEBAR_STATE_DIR remains the escape hatch for any layout not covered here,
 * matching the override the rest of the plugin already accepts.
 *
 * Writer (the statusLine bridge) and reader (the provider) resolve through this
 * same function, so the pair stays consistent whatever the layout.
 */
package cursor

import (
	"os"
	"path/filepath"
)

// stateDirName is the plugin-owned subdirectory inside the agent's config dir,
// matching the convention used for every other agent's derived state.
const stateDirName = "herdr-usagebar"

// sessionsDirName holds one snapshot per Cursor session id.
const sessionsDirName = "sessions"

// ConfigDir returns Cursor's CLI configuration directory.
func ConfigDir() string {
	if dir := os.Getenv("CURSOR_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cursor")
}

// StateDir returns the directory holding Cursor's plugin-derived state.
func StateDir() string {
	if dir := os.Getenv("USAGEBAR_STATE_DIR"); dir != "" {
		return dir
	}
	configDir := ConfigDir()
	if configDir == "" {
		return ""
	}
	return filepath.Join(configDir, stateDirName)
}

// SessionsDir returns the directory holding per-session snapshots.
func SessionsDir(stateDir string) string {
	return filepath.Join(stateDir, sessionsDirName)
}
