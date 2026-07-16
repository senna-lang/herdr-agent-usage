/**
 * Resolves the path to the OpenCode SQLite database.
 * Default: $XDG_DATA_HOME/opencode/opencode.db or ~/.local/share/opencode/opencode.db
 * Overrides: OPENCODE_DATA_DIR (a directory) or OPENCODE_DB (a file path).
 */
package opencode

import (
	"os"
	"path/filepath"
)

// ResolveOpenCodeDBPath returns the DB path if it exists.
func ResolveOpenCodeDBPath() string {
	if explicit := os.Getenv("OPENCODE_DB"); explicit != "" {
		if st, err := os.Stat(explicit); err == nil && st.Mode().IsRegular() {
			return explicit
		}
		return ""
	}
	if dir := os.Getenv("OPENCODE_DATA_DIR"); dir != "" {
		path := filepath.Join(dir, "opencode.db")
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			return path
		}
		return ""
	}
	base := ""
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		base = filepath.Join(xdg, "opencode")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "share", "opencode")
	}
	path := filepath.Join(base, "opencode.db")
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
		return path
	}
	return ""
}
