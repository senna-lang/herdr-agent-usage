package omp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodePiSessionDir(t *testing.T) {
	got := EncodePiSessionDir("/Users/megablacklabel")
	want := "--Users-megablacklabel--"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFindOMPSessionForCwdByFilename_RehomesStalePath(t *testing.T) {
	root := t.TempDir()
	cwd := "/Users/senna/Documents/Repos/life"
	filename := "2026-08-10T01-06-25-252Z_019fe934-e364-7000-a8e5-6c59127e8a7a.jsonl"
	wantDir := filepath.Join(root, EncodeOMPSessionDir(cwd))
	if err := os.MkdirAll(wantDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(wantDir, filename)
	if err := os.WriteFile(want, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMP_SESSIONS_ROOT", root)

	got := FindOMPSessionForCwdByFilename(cwd, filepath.Join(root, "home-life-legacy", filename))
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFindLatestSessionInDir(t *testing.T) {
	dir := t.TempDir()
	older := filepath.Join(dir, "2026-01-01T00-00-00Z_old.jsonl")
	newer := filepath.Join(dir, "2026-01-02T00-00-00Z_new.jsonl")
	if err := os.WriteFile(older, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(newer, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := FindLatestSessionInDir(dir)
	if got != newer {
		t.Fatalf("got %q want %q", got, newer)
	}
}
