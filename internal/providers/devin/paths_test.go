/**
 * Tests for Devin path resolution.
 *
 * Devin shares the XDG data root with every other CLI that lives there, so
 * the property that matters is that resolution lands inside the devin
 * directory under each root and nowhere else.
 */
package devin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDevinDataDir_EnvironmentOverrides(t *testing.T) {
	dir := t.TempDir()

	t.Setenv("DEVIN_DATA_DIR", dir)
	if got := ResolveDevinDataDir(); got != dir {
		t.Fatalf("DEVIN_DATA_DIR not honoured: %q", got)
	}

	// XDG_DATA_HOME moves the devin directory with the data root.
	t.Setenv("DEVIN_DATA_DIR", "")
	xdg := filepath.Join(dir, "xdg")
	devinDir := filepath.Join(xdg, "devin")
	if err := os.MkdirAll(devinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", xdg)
	if got := ResolveDevinDataDir(); got != devinDir {
		t.Fatalf("XDG_DATA_HOME not honoured: %q", got)
	}

	// A data root without a devin directory resolves to nothing rather than
	// to a path the caller would then fail to open.
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "elsewhere"))
	if got := ResolveDevinDataDir(); got != "" {
		t.Fatalf("absent devin dir should resolve empty, got %q", got)
	}
}

func TestResolveDevinPaths_StayInsideDevinDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("DEVIN_DATA_DIR", "")

	data := filepath.Join(home, ".local", "share", "devin")
	if err := os.MkdirAll(filepath.Join(data, "cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(data, "cli", "sessions.db"),
		filepath.Join(data, "credentials.toml"),
	} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if got := ResolveDevinDataDir(); got != data {
		t.Fatalf("data dir = %q", got)
	}
	if got := ResolveDevinSessionsDBPath(); got != filepath.Join(data, "cli", "sessions.db") {
		t.Fatalf("sessions.db = %q", got)
	}
	if got := ResolveDevinCredentialsPath(); got != filepath.Join(data, "credentials.toml") {
		t.Fatalf("credentials.toml = %q", got)
	}
}

func TestResolveDevinPathsIn_IgnoresProcessEnvironment(t *testing.T) {
	// A directory-scoped resolver must not follow DEVIN_DATA_DIR, or a caller
	// holding an explicit root would read the ambient machine's store.
	other := t.TempDir()
	if err := os.MkdirAll(filepath.Join(other, "cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(other, "cli", "sessions.db")
	if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVIN_DATA_DIR", t.TempDir())
	if got := ResolveDevinSessionsDBPathIn(other); got != db {
		t.Fatalf("ResolveDevinSessionsDBPathIn followed the environment: %q", got)
	}
	if got := ResolveDevinSessionsDBPathIn(filepath.Join(other, "absent")); got != "" {
		t.Fatalf("absent store should resolve empty, got %q", got)
	}
	if got := ResolveDevinSessionsDBPathIn(""); got != "" {
		t.Fatalf("empty root should resolve empty, got %q", got)
	}
}
