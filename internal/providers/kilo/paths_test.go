/**
 * Tests for Kilo path resolution.
 *
 * Kilo shares OpenCode's on-disk layout but keeps its own directories, so the
 * property that matters most is that this reader never resolves into
 * OpenCode's store on a machine that has both installed.
 */
package kilo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveKiloDBPath_EnvironmentOverrides(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "kilo.db")
	if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("KILO_DB", db)
	if got := ResolveKiloDBPath(); got != db {
		t.Fatalf("KILO_DB not honoured: %q", got)
	}

	t.Setenv("KILO_DB", "")
	t.Setenv("KILO_DATA_DIR", dir)
	if got := ResolveKiloDBPath(); got != db {
		t.Fatalf("KILO_DATA_DIR not honoured: %q", got)
	}

	// A path that does not exist resolves to nothing rather than to a path
	// the caller would then fail to open.
	t.Setenv("KILO_DATA_DIR", "")
	t.Setenv("KILO_DB", filepath.Join(dir, "absent.db"))
	if got := ResolveKiloDBPath(); got != "" {
		t.Fatalf("missing file should resolve empty, got %q", got)
	}
}

func TestResolveKiloDBPathIn_IgnoresProcessEnvironment(t *testing.T) {
	// A profile-scoped resolver must not follow KILO_DB, or two configured
	// profiles would read each other's store.
	other := t.TempDir()
	db := filepath.Join(other, "kilo.db")
	if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KILO_DB", filepath.Join(t.TempDir(), "elsewhere.db"))
	if got := ResolveKiloDBPathIn(other); got != db {
		t.Fatalf("ResolveKiloDBPathIn followed the environment: %q", got)
	}
	if got := ResolveKiloDBPathIn(filepath.Join(other, "absent")); got != "" {
		t.Fatalf("absent store should resolve empty, got %q", got)
	}
}

func TestResolveKiloPaths_StayOutOfOpenCodesStore(t *testing.T) {
	// The default layout must be Kilo's own directory under every root, never
	// OpenCode's, or a machine with both CLIs would cross-read stores.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("KILO_DB", "")
	t.Setenv("KILO_DATA_DIR", "")
	t.Setenv("KILO_MODELS_PATH", "")

	data := filepath.Join(home, ".local", "share", "kilo")
	cache := filepath.Join(home, ".cache", "kilo")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(data, "kilo.db"),
		filepath.Join(data, "auth.json"),
		filepath.Join(cache, "models.json"),
	} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if got := ResolveKiloDBPath(); got != filepath.Join(data, "kilo.db") {
		t.Fatalf("db = %q", got)
	}
	if got := ResolveKiloAuthPath(); got != filepath.Join(data, "auth.json") {
		t.Fatalf("auth = %q", got)
	}
	if got := ResolveKiloModelsPath(); got != filepath.Join(cache, "models.json") {
		t.Fatalf("models = %q", got)
	}
	for _, got := range []string{ResolveKiloDBPath(), ResolveKiloAuthPath(), ResolveKiloModelsPath()} {
		if filepath.Base(filepath.Dir(got)) != "kilo" {
			t.Fatalf("resolved outside a kilo directory: %q", got)
		}
	}
}
