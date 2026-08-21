/**
 * Tests for Cursor snapshot persistence, including write atomicity.
 */
package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func windowOf(n int) *int { return &n }

func TestWriteSnapshot_RoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	want := Snapshot{
		SessionID: "s1", PaneID: "w1:p1", Model: "Auto", Cwd: "/w",
		ContextTokens: 42752, WindowTokens: windowOf(256000), UpdatedAtMs: 1700,
	}
	if err := WriteSnapshot(dir, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadSnapshot(dir, "s1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SessionID != want.SessionID || got.ContextTokens != want.ContextTokens ||
		got.PaneID != want.PaneID || got.UpdatedAtMs != want.UpdatedAtMs {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got.WindowTokens == nil || *got.WindowTokens != 256000 {
		t.Fatalf("WindowTokens = %v", got.WindowTokens)
	}
}

// The statusLine contract kills an in-flight command whenever a newer update
// arrives, so a partially written snapshot must never be observable. Writing
// concurrently while reading must therefore only ever yield whole snapshots.
func TestWriteSnapshot_IsAtomicUnderConcurrentReaders(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	base := Snapshot{SessionID: "s1", PaneID: "w1:p1", ContextTokens: 1, UpdatedAtMs: 1}
	if err := WriteSnapshot(dir, base); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 2; i < 200; i++ {
			snap := base
			snap.ContextTokens = i
			snap.UpdatedAtMs = int64(i)
			_ = WriteSnapshot(dir, snap)
		}
		close(stop)
	}()

	reads := 0
	for {
		select {
		case <-stop:
			wg.Wait()
			if reads == 0 {
				t.Fatal("no concurrent reads observed")
			}
			return
		default:
			// A torn write would surface as a JSON error or a zero-valued
			// snapshot; neither is acceptable at any point.
			snap, err := ReadSnapshot(dir, "s1")
			if err != nil {
				t.Fatalf("observed unreadable snapshot mid-write: %v", err)
			}
			if snap.SessionID != "s1" || snap.ContextTokens == 0 {
				t.Fatalf("observed partial snapshot: %+v", snap)
			}
			reads++
		}
	}
}

// A rename-based write must not leave working files behind for the listing to
// trip over, however many times it runs.
func TestWriteSnapshot_LeavesNoTempFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	for i := 0; i < 25; i++ {
		if err := WriteSnapshot(dir, Snapshot{SessionID: "s1", ContextTokens: i, UpdatedAtMs: int64(i)}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") || strings.HasPrefix(entry.Name(), ".snapshot-") {
			t.Errorf("leftover temp file %q", entry.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries, want exactly the snapshot", len(entries))
	}
}

// One corrupt file must not hide every other pane's usage.
func TestListSnapshots_SkipsUnreadableEntries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if err := WriteSnapshot(dir, Snapshot{SessionID: "good", PaneID: "w1:p1", ContextTokens: 5, UpdatedAtMs: 1}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write non-json: %v", err)
	}
	got := ListSnapshots(dir)
	if len(got) != 1 || got[0].SessionID != "good" {
		t.Fatalf("got %+v, want only the readable snapshot", got)
	}
}

func TestListSnapshots_MissingDirectory(t *testing.T) {
	if got := ListSnapshots(filepath.Join(t.TempDir(), "absent")); got != nil {
		t.Fatalf("got %+v, want nil for a missing directory", got)
	}
}

func TestRemoveSnapshot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if err := WriteSnapshot(dir, Snapshot{SessionID: "s1", UpdatedAtMs: 1}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := RemoveSnapshot(dir, "s1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := ReadSnapshot(dir, "s1"); err == nil {
		t.Fatal("snapshot still readable after removal")
	}
	// Teardown may run when no snapshot was ever written; that is not an error.
	if err := RemoveSnapshot(dir, "s1"); err != nil {
		t.Fatalf("second remove: %v", err)
	}
}

func TestSnapshot_JSONShapeIsStable(t *testing.T) {
	encoded, err := json.Marshal(Snapshot{SessionID: "s1", ContextTokens: 5, UpdatedAtMs: 7})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"session_id"`, `"context_tokens"`, `"updated_at_ms"`} {
		if !strings.Contains(string(encoded), key) {
			t.Errorf("missing %s in %s", key, encoded)
		}
	}
}
