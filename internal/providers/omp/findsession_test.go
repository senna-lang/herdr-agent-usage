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
