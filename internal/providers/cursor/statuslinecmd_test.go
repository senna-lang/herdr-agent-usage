/**
 * Tests for the Cursor statusLine command behaviour.
 */
package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunStatusLineIn_RecordsAndRenders(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	text, err := RunStatusLineIn(dir, []byte(capturedPayload), "w1:p1", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text == "" {
		t.Fatal("rendered no status line text")
	}
	snap, err := ReadSnapshot(dir, "11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatalf("snapshot not recorded: %v", err)
	}
	if snap.ContextTokens != 42752 || snap.PaneID != "w1:p1" {
		t.Fatalf("recorded %+v", snap)
	}
}

// The central guarantee: an unusable update must leave the last good snapshot
// exactly as it was, rather than blanking or truncating it.
func TestRunStatusLineIn_BadPayloadPreservesLastGoodSnapshot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if _, err := RunStatusLineIn(dir, []byte(capturedPayload), "w1:p1", now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Compare encoded bytes: Snapshot holds a pointer field, so struct equality
	// would compare pointer identity rather than the recorded values.
	before, err := os.ReadFile(filepath.Join(dir, "11111111-2222-3333-4444-555555555555.json"))
	if err != nil {
		t.Fatalf("seed read: %v", err)
	}

	for _, payload := range []string{
		"",
		"   ",
		"garbage",
		`{"session_id":"11111111-2222-3333-4444-555555555555","context_window":{`,
		`{"session_id":"11111111-2222-3333-4444-555555555555","context_window":{"total_input_tokens":null}}`,
		`{"context_window":{"total_input_tokens":123}}`,
	} {
		if _, err := RunStatusLineIn(dir, []byte(payload), "w1:p1", now+1); err == nil {
			t.Fatalf("payload %q: expected an error", payload)
		}
		after, err := os.ReadFile(filepath.Join(dir, "11111111-2222-3333-4444-555555555555.json"))
		if err != nil {
			t.Fatalf("payload %q: snapshot became unreadable: %v", payload, err)
		}
		if string(after) != string(before) {
			t.Fatalf("payload %q: snapshot changed from %s to %s", payload, before, after)
		}
	}
}

func TestRunStatusLineIn_PrunesStaleSnapshots(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	stale := Snapshot{SessionID: "old", PaneID: "w1:p1", ContextTokens: 1, UpdatedAtMs: now - SnapshotFreshnessMs - 1}
	recent := Snapshot{SessionID: "recent", PaneID: "w1:p1", ContextTokens: 2, UpdatedAtMs: now - 1000}
	for _, snap := range []Snapshot{stale, recent} {
		if err := WriteSnapshot(dir, snap); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if _, err := RunStatusLineIn(dir, []byte(capturedPayload), "w1:p1", now); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := ReadSnapshot(dir, "old"); err == nil {
		t.Error("stale snapshot was not pruned")
	}
	if _, err := ReadSnapshot(dir, "recent"); err != nil {
		t.Errorf("still-resolvable snapshot was pruned: %v", err)
	}
}

// Cursor renders stdout as the status line, so the text must describe the same
// usage the herdr $context row does.
func TestStatusLineText_UsesSharedContextFormatting(t *testing.T) {
	text := statusLineText(Snapshot{Model: "Auto", ContextTokens: 42752, WindowTokens: windowOf(256000)})
	if text != "Auto  ⛁ 17% (43k)" {
		t.Fatalf("got %q", text)
	}
	// Without a model label the line is the context text alone.
	if got := statusLineText(Snapshot{ContextTokens: 900}); got != "⛁ 900" {
		t.Fatalf("got %q", got)
	}
}
