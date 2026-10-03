/**
 * Resolves the paths to Kilo Code's local stores.
 *
 * Kilo is an OpenCode fork and kept the same on-disk layout under its own
 * directories, so these mirror the OpenCode resolvers exactly: a machine with
 * both CLIs installed keeps two independent stores and this never reads
 * OpenCode's.
 *
 *   session store  $XDG_DATA_HOME/kilo/kilo.db   (default ~/.local/share/kilo/kilo.db)
 *   credentials    $XDG_DATA_HOME/kilo/auth.json (default ~/.local/share/kilo/auth.json)
 *   model catalog  $XDG_CACHE_HOME/kilo/models.json (default ~/.cache/kilo/models.json)
 *
 * Overrides: KILO_DB / KILO_DATA_DIR and KILO_MODELS_PATH.
 */
package kilo

import (
	"os"
	"path/filepath"
)

// dataDirName is Kilo's own data directory name under XDG_DATA_HOME.
const dataDirName = "kilo"

// ResolveKiloDBPath returns the session store path if it exists.
func ResolveKiloDBPath() string {
	if explicit := os.Getenv("KILO_DB"); explicit != "" {
		return regularFile(explicit)
	}
	if dir := os.Getenv("KILO_DATA_DIR"); dir != "" {
		return ResolveKiloDBPathIn(dir)
	}
	base := ""
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		base = filepath.Join(xdg, dataDirName)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "share", dataDirName)
	}
	return ResolveKiloDBPathIn(base)
}

// ResolveKiloDBPathIn returns the database owned by one configured Kilo data
// directory without consulting process-wide environment overrides.
func ResolveKiloDBPathIn(dataDir string) string {
	return regularFile(filepath.Join(dataDir, "kilo.db"))
}

// ResolveKiloAuthPath returns the credential store path if it exists.
func ResolveKiloAuthPath() string {
	if dir := os.Getenv("KILO_DATA_DIR"); dir != "" {
		return regularFile(filepath.Join(dir, "auth.json"))
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return regularFile(filepath.Join(xdg, dataDirName, "auth.json"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return regularFile(filepath.Join(home, ".local", "share", dataDirName, "auth.json"))
}

// ResolveKiloModelsPath returns the model catalog path if it exists.
//
// The catalog is a large cache Kilo refreshes itself; a missing file is normal
// on a machine that has never opened Kilo, and resolution then simply reports
// no context window.
func ResolveKiloModelsPath() string {
	if explicit := os.Getenv("KILO_MODELS_PATH"); explicit != "" {
		return regularFile(explicit)
	}
	base := ""
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		base = filepath.Join(xdg, dataDirName)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache", dataDirName)
	}
	return regularFile(filepath.Join(base, "models.json"))
}

func regularFile(path string) string {
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
		return path
	}
	return ""
}
