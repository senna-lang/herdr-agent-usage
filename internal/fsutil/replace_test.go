/**
 * Tests for atomic replacement while readers hold the destination open.
 */
package fsutil

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// A cache consumer may hold the file open at the exact moment the statusLine
// writer replaces it. The replacement must still succeed on every platform,
// the old handle must keep seeing the old content, and new reads must see the
// new content.
func TestReplaceFile_SucceedsWhileReaderHoldsDestinationOpen(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "cache.json")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	reader, err := openShared(dst)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	defer reader.Close()

	src := filepath.Join(dir, "cache.json.tmp")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	if err := ReplaceFile(src, dst); err != nil {
		t.Fatalf("replace while reader open: %v", err)
	}

	held, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read held handle: %v", err)
	}
	if string(held) != "old" {
		t.Fatalf("held handle = %q, want the pre-replace content", held)
	}
	fresh, err := ReadFile(dst)
	if err != nil {
		t.Fatalf("read after replace: %v", err)
	}
	if string(fresh) != "new" {
		t.Fatalf("fresh read = %q, want the replaced content", fresh)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("temp file must be gone after replace, stat err = %v", err)
	}
}

func TestReadFile_MissingFileReportsNotExist(t *testing.T) {
	_, err := ReadFile(filepath.Join(t.TempDir(), "absent.json"))
	if !os.IsNotExist(err) {
		t.Fatalf("err = %v, want not-exist", err)
	}
}
