/**
 * Tests for Antigravity snapshot persistence, including write atomicity.
 */
package antigravity

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
		SessionID:     "s1",
		PaneID:        "w1:p1",
		ContextTokens: 18558,
		WindowTokens:  windowOf(1048576),
		UpdatedAtMs:   1000,
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
	if got.WindowTokens == nil || *got.WindowTokens != 1048576 {
		t.Fatalf("WindowTokens = %v", got.WindowTokens)
	}
}

// The statusLine contract kills an in-flight command whenever a newer update
// arrives, so a partially written snapshot must never be observable. Writing
// concurrently while reading must therefore only ever yield whole snapshots.
func TestWriteSnapshot_IsAtomicUnderConcurrentReaders(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if err := WriteSnapshot(dir, Snapshot{SessionID: "s1", ContextTokens: 1, UpdatedAtMs: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	var readErr error
	var mu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			raw, err := os.ReadFile(filepath.Join(dir, "s1.json"))
			if err != nil {
				continue
			}
			var snap Snapshot
			if err := json.Unmarshal(raw, &snap); err != nil {
				mu.Lock()
				readErr = err
				mu.Unlock()
				return
			}
		}
	}()

	for i := range 200 {
		if err := WriteSnapshot(dir, Snapshot{SessionID: "s1", ContextTokens: i, UpdatedAtMs: int64(i)}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if readErr != nil {
		t.Fatalf("reader observed a partial write: %v", readErr)
	}
}

// A rename-based write must not leave working files behind for the listing to
// trip over, however many times it runs.
func TestWriteSnapshot_LeavesNoTempFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	for i := range 10 {
		if err := WriteSnapshot(dir, Snapshot{SessionID: "s1", ContextTokens: i, UpdatedAtMs: int64(i)}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatalf("leftover temp file: %s", entry.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want exactly the one snapshot", len(entries))
	}
}

// One corrupt file must not hide every other pane's usage.
func TestListSnapshots_SkipsUnreadableEntries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if err := WriteSnapshot(dir, Snapshot{SessionID: "good", ContextTokens: 1, UpdatedAtMs: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt: %v", err)
	}
	snaps := ListSnapshots(dir)
	if len(snaps) != 1 || snaps[0].SessionID != "good" {
		t.Fatalf("got %+v, want only the good snapshot", snaps)
	}
}

func TestListSnapshots_MissingDirectory(t *testing.T) {
	if got := ListSnapshots(filepath.Join(t.TempDir(), "missing")); got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

func TestRemoveSnapshot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if err := WriteSnapshot(dir, Snapshot{SessionID: "s1", ContextTokens: 1, UpdatedAtMs: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := RemoveSnapshot(dir, "s1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := ReadSnapshot(dir, "s1"); err == nil {
		t.Fatal("expected an error reading a removed snapshot")
	}
	// Removing an already-absent snapshot is not an error.
	if err := RemoveSnapshot(dir, "s1"); err != nil {
		t.Fatalf("remove absent: %v", err)
	}
}

func TestSnapshot_JSONShapeIsStable(t *testing.T) {
	snap := Snapshot{
		SessionID:     "s1",
		PaneID:        "w1:p1",
		Model:         "Gemini 3.8 Flash (High)",
		Cwd:           "/repo",
		ContextTokens: 18558,
		WindowTokens:  windowOf(1048576),
		Quota: map[string]QuotaWindow{
			"gemini-weekly": {RemainingFraction: 0.99, ResetTime: "2026-09-22T10:06:55Z"},
		},
		PlanTier:    "Antigravity Starter Quota",
		Email:       "user@example.com",
		UpdatedAtMs: 1000,
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{
		`"session_id"`, `"pane_id"`, `"model"`, `"cwd"`, `"context_tokens"`,
		`"window_tokens"`, `"quota"`, `"plan_tier"`, `"email"`, `"updated_at_ms"`,
	} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("marshaled snapshot missing key %s: %s", key, raw)
		}
	}
}

func TestPruneStale_RemovesOnlyStaleSnapshots(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if err := WriteSnapshot(dir, Snapshot{SessionID: "old", ContextTokens: 1, UpdatedAtMs: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WriteSnapshot(dir, Snapshot{SessionID: "recent", ContextTokens: 1, UpdatedAtMs: 900}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	PruneStale(dir, 1000, 500)
	if _, err := ReadSnapshot(dir, "old"); err == nil {
		t.Error("stale snapshot was not pruned")
	}
	if _, err := ReadSnapshot(dir, "recent"); err != nil {
		t.Errorf("still-resolvable snapshot was pruned: %v", err)
	}
}
