/**
 * Tests for cwd normalization and equality.
 */
package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEqualExact(t *testing.T) {
	if !Equal("/a/b", "/a/b") {
		t.Fatal("exact")
	}
}

func TestEqualTrailingSlash(t *testing.T) {
	if !Equal("/Users/me/proj", "/Users/me/proj/") {
		t.Fatal("trailing slash")
	}
}

func TestEqualPrivatePrefix(t *testing.T) {
	a := "/var/folders/xy/tmp"
	b := "/private/var/folders/xy/tmp"
	if !Equal(a, b) {
		t.Fatalf("private prefix: %q vs %q (norm %q %q)", a, b, Normalize(a), Normalize(b))
	}
}

func TestEqualSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real-project")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link-project")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlink not supported")
	}
	if !Equal(real, link) {
		t.Fatalf("symlink: real=%q link=%q", Normalize(real), Normalize(link))
	}
}

func TestSameProjectBasename(t *testing.T) {
	if !SameProject("/old/parent/my-app", "/new/parent/my-app") {
		t.Fatal("basename")
	}
	if SameProject("/a/foo", "/b/bar") {
		t.Fatal("different bases")
	}
}

func TestBaseName(t *testing.T) {
	if BaseName("/x/y/z") != "z" {
		t.Fatal(BaseName("/x/y/z"))
	}
	if BaseName("") != "" {
		t.Fatal("empty")
	}
}

func TestSameProjectArchiveRename(t *testing.T) {
	if !SameProject("/Users/x/herdr-usagebar", "/Users/y/herdr-usagebar-ts-archived") {
		t.Fatal("archive rename")
	}
	if SameProject("/Users/x/app", "/Users/y/apple") {
		t.Fatal("false positive")
	}
}
