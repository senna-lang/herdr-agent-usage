/**
 * Resolves the paths to Devin CLI's local stores.
 *
 * Devin CLI keeps its data under the XDG data directory:
 *
 *   session store   $XDG_DATA_HOME/devin/cli/sessions.db   (default ~/.local/share/devin/cli/sessions.db)
 *   credentials     $XDG_DATA_HOME/devin/credentials.toml  (default ~/.local/share/devin/credentials.toml)
 *
 * Overrides: DEVIN_DATA_DIR relocates the whole data directory.
 */
package devin

import (
	"os"
	"path/filepath"
)

// ResolveDevinDataDir returns Devin CLI's data directory if it exists.
func ResolveDevinDataDir() string {
	if dir := os.Getenv("DEVIN_DATA_DIR"); dir != "" {
		return dir
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return regularDir(filepath.Join(xdg, "devin"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return regularDir(filepath.Join(home, ".local", "share", "devin"))
}

// ResolveDevinSessionsDBPath returns the session store path if it exists.
func ResolveDevinSessionsDBPath() string {
	return ResolveDevinSessionsDBPathIn(ResolveDevinDataDir())
}

// ResolveDevinSessionsDBPathIn returns the session store under one data
// directory without consulting process-wide environment overrides.
func ResolveDevinSessionsDBPathIn(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return regularFile(filepath.Join(dataDir, "cli", "sessions.db"))
}

// ResolveDevinCredentialsPath returns the credential store path if it exists.
func ResolveDevinCredentialsPath() string {
	return ResolveDevinCredentialsPathIn(ResolveDevinDataDir())
}

// ResolveDevinCredentialsPathIn returns the credential store under one data
// directory without consulting process-wide environment overrides.
func ResolveDevinCredentialsPathIn(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return regularFile(filepath.Join(dataDir, "credentials.toml"))
}

func regularDir(path string) string {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return path
	}
	return ""
}

func regularFile(path string) string {
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
		return path
	}
	return ""
}
