package updatecheck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestVersion_ReadsDeclaredVersion(t *testing.T) {
	root := t.TempDir()
	manifest := "id = \"usagebar\"\nversion = \"0.5.16\"\n\n[[actions]]\nid = \"refresh\"\n"
	if err := os.WriteFile(filepath.Join(root, ManifestFileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ManifestVersion(root); got != "0.5.16" {
		t.Fatalf("got %q, want 0.5.16", got)
	}
}

func TestManifestVersion_MissingOrEmptyRootYieldsEmpty(t *testing.T) {
	if got := ManifestVersion(""); got != "" {
		t.Fatalf("empty root: got %q", got)
	}
	if got := ManifestVersion(t.TempDir()); got != "" {
		t.Fatalf("missing manifest: got %q", got)
	}
}

// The repository's own manifest must stay readable by this parser, since the
// Windows hooks rely on it instead of a wrapper script.
func TestManifestVersion_RepositoryManifest(t *testing.T) {
	if got := ManifestVersion(filepath.Join("..", "..")); got == "" {
		t.Fatal("repository herdr-plugin.toml declares no readable version")
	}
}
