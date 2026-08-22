/**
 * Tests for Cursor context resolution: session identity, pane fallback,
 * freshness, and the cases that must resolve to no usage at all.
 */
package cursor

import (
	"path/filepath"
	"testing"
)

const now int64 = 1_000_000_000

func strPtr(s string) *string { return &s }

// seed writes snapshots into a fresh sessions dir and returns its path.
func seed(t *testing.T, snaps ...Snapshot) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sessions")
	for _, snap := range snaps {
		if err := WriteSnapshot(dir, snap); err != nil {
			t.Fatalf("seed %s: %v", snap.SessionID, err)
		}
	}
	return dir
}

func TestResolve_ExactSessionMatch(t *testing.T) {
	dir := seed(t, Snapshot{
		SessionID: "live", PaneID: "w1:p1", Cwd: "/repo",
		ContextTokens: 42752, WindowTokens: windowOf(256000), UpdatedAtMs: now,
	})
	usage := ResolveUsageIn(dir, strPtr("live"), strPtr("w1:p1"), strPtr("/repo"), now)
	if usage == nil {
		t.Fatal("got nil, want usage")
	}
	if usage.ContextTokens != 42752 || usage.WindowTokens == nil || *usage.WindowTokens != 256000 {
		t.Fatalf("got %+v", usage)
	}
}

// After /clear, Cursor writes snapshots under a new session id while herdr keeps
// reporting the one it saw at launch. Pane identity must still resolve it.
func TestResolve_ClearedSessionResolvesByPane(t *testing.T) {
	dir := seed(t,
		Snapshot{SessionID: "before-clear", PaneID: "w1:p1", Cwd: "/repo", ContextTokens: 90000, WindowTokens: windowOf(256000), UpdatedAtMs: now - 60_000},
		Snapshot{SessionID: "after-clear", PaneID: "w1:p1", Cwd: "/repo", ContextTokens: 1200, WindowTokens: windowOf(256000), UpdatedAtMs: now},
	)
	// herdr still reports the pre-clear session, which no longer has the newest
	// snapshot; the pane fallback must select the post-clear one.
	usage := ResolveUsageIn(dir, strPtr("stale-unknown-session"), strPtr("w1:p1"), strPtr("/repo"), now)
	if usage == nil {
		t.Fatal("got nil, want the post-clear snapshot")
	}
	if usage.ContextTokens != 1200 {
		t.Fatalf("ContextTokens = %d, want the newest (1200)", usage.ContextTokens)
	}
}

// The reported session's snapshot usually still exists after /clear and is
// recent enough to pass the freshness bound, so recency on the pane must beat
// the reported identity. Reproduced from a live two-pane smoke test, where
// resolving by the reported session alone returned the pre-clear context.
func TestResolve_ClearedSessionSupersedesTheReportedOne(t *testing.T) {
	dir := seed(t,
		// herdr still reports this one; it is recent, but superseded.
		Snapshot{SessionID: "reported", PaneID: "w1:p1", Cwd: "/repo", ContextTokens: 26368, WindowTokens: windowOf(256000), UpdatedAtMs: now - 90_000},
		Snapshot{SessionID: "after-clear", PaneID: "w1:p1", Cwd: "/repo", ContextTokens: 20736, WindowTokens: windowOf(256000), UpdatedAtMs: now},
	)
	usage := ResolveUsageIn(dir, strPtr("reported"), strPtr("w1:p1"), strPtr("/repo"), now)
	if usage == nil {
		t.Fatal("got nil, want the post-clear snapshot")
	}
	if usage.ContextTokens != 20736 {
		t.Fatalf("ContextTokens = %d, want the post-clear value 20736", usage.ContextTokens)
	}
}

// Supersession needs a pane to compare within: with no pane id the reported
// session stays the best available answer rather than resolving to nothing.
func TestResolve_ReportedSessionKeptWhenPaneUnknown(t *testing.T) {
	dir := seed(t,
		Snapshot{SessionID: "reported", PaneID: "w1:p1", Cwd: "/repo", ContextTokens: 26368, UpdatedAtMs: now - 90_000},
		Snapshot{SessionID: "after-clear", PaneID: "w1:p1", Cwd: "/repo", ContextTokens: 20736, UpdatedAtMs: now},
	)
	usage := ResolveUsageIn(dir, strPtr("reported"), nil, nil, now)
	if usage == nil || usage.ContextTokens != 26368 {
		t.Fatalf("got %+v, want the reported session's own snapshot", usage)
	}
}

// A newer snapshot belonging to a different pane must not displace this one.
func TestResolve_SupersessionIsScopedToTheSamePane(t *testing.T) {
	dir := seed(t,
		Snapshot{SessionID: "mine", PaneID: "w1:pA", Cwd: "/repo", ContextTokens: 26368, UpdatedAtMs: now - 90_000},
		Snapshot{SessionID: "neighbour", PaneID: "w1:pB", Cwd: "/repo", ContextTokens: 20736, UpdatedAtMs: now},
	)
	usage := ResolveUsageIn(dir, strPtr("mine"), strPtr("w1:pA"), strPtr("/repo"), now)
	if usage == nil || usage.ContextTokens != 26368 {
		t.Fatalf("got %+v, want this pane's own snapshot", usage)
	}
}

// Two Cursor panes in one repository must never borrow each other's context.
// cwd alone cannot tell them apart, which is why resolution is pane-keyed.
func TestResolve_TwoPanesSharingCwdDoNotCrossAttribute(t *testing.T) {
	dir := seed(t,
		Snapshot{SessionID: "s-a", PaneID: "w1:pA", Cwd: "/repo", ContextTokens: 1000, WindowTokens: windowOf(256000), UpdatedAtMs: now},
		Snapshot{SessionID: "s-b", PaneID: "w1:pB", Cwd: "/repo", ContextTokens: 2000, WindowTokens: windowOf(256000), UpdatedAtMs: now},
	)
	for _, tc := range []struct {
		pane string
		want int
	}{{"w1:pA", 1000}, {"w1:pB", 2000}} {
		usage := ResolveUsageIn(dir, nil, strPtr(tc.pane), strPtr("/repo"), now)
		if usage == nil || usage.ContextTokens != tc.want {
			t.Fatalf("%s: got %+v, want %d", tc.pane, usage, tc.want)
		}
	}
	// A third pane in the same directory has no snapshot of its own and must
	// not adopt either neighbour's.
	if usage := ResolveUsageIn(dir, nil, strPtr("w1:pC"), strPtr("/repo"), now); usage != nil {
		t.Fatalf("got %+v for a pane with no snapshot, want nil", usage)
	}
}

func TestResolve_StaleReturnsNoUsage(t *testing.T) {
	stale := now - SnapshotFreshnessMs - 1
	dir := seed(t, Snapshot{SessionID: "s1", PaneID: "w1:p1", Cwd: "/repo", ContextTokens: 5000, UpdatedAtMs: stale})
	if usage := ResolveUsageIn(dir, strPtr("s1"), strPtr("w1:p1"), strPtr("/repo"), now); usage != nil {
		t.Fatalf("got %+v for a stale snapshot, want nil", usage)
	}
}

func TestResolve_FreshnessBoundaryIsInclusive(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "s1", PaneID: "w1:p1", ContextTokens: 5000, UpdatedAtMs: now - SnapshotFreshnessMs})
	if usage := ResolveUsageIn(dir, strPtr("s1"), nil, nil, now); usage == nil {
		t.Fatal("got nil exactly at the freshness bound, want usage")
	}
}

// A timestamp in the future indicates a clock the resolver cannot reason about;
// it must not be treated as the freshest possible snapshot.
func TestResolve_FutureTimestampIsNotFresh(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "s1", PaneID: "w1:p1", ContextTokens: 5000, UpdatedAtMs: now + 60_000})
	if usage := ResolveUsageIn(dir, strPtr("s1"), strPtr("w1:p1"), nil, now); usage != nil {
		t.Fatalf("got %+v for a future-dated snapshot, want nil", usage)
	}
}

// Two sessions claiming one pane at the same instant give no basis to prefer
// either, so neither is shown.
func TestResolve_AmbiguousPaneMatchReturnsNoUsage(t *testing.T) {
	dir := seed(t,
		Snapshot{SessionID: "s-a", PaneID: "w1:p1", Cwd: "/repo", ContextTokens: 1000, UpdatedAtMs: now},
		Snapshot{SessionID: "s-b", PaneID: "w1:p1", Cwd: "/repo", ContextTokens: 2000, UpdatedAtMs: now},
	)
	if usage := ResolveUsageIn(dir, nil, strPtr("w1:p1"), strPtr("/repo"), now); usage != nil {
		t.Fatalf("got %+v for an ambiguous match, want nil", usage)
	}
}

// A snapshot left by a previous occupant of a reused pane id is rejected when
// its working directory contradicts the pane's.
func TestResolve_PaneFallbackRequiresConsistentCwd(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "s1", PaneID: "w1:p1", Cwd: "/other-repo", ContextTokens: 1000, UpdatedAtMs: now})
	if usage := ResolveUsageIn(dir, nil, strPtr("w1:p1"), strPtr("/repo"), now); usage != nil {
		t.Fatalf("got %+v for a cwd mismatch, want nil", usage)
	}
	// An unknown cwd on either side cannot contradict the other.
	if usage := ResolveUsageIn(dir, nil, strPtr("w1:p1"), nil, now); usage == nil {
		t.Fatal("got nil when the pane cwd is unknown, want usage")
	}
}

func TestResolve_NoIdentifiersReturnsNoUsage(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "s1", PaneID: "w1:p1", ContextTokens: 1000, UpdatedAtMs: now})
	for _, tc := range []struct {
		name          string
		session, pane *string
	}{
		{"neither", nil, nil},
		{"empty strings", strPtr(""), strPtr("")},
		{"unknown session only", strPtr("nope"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if usage := ResolveUsageIn(dir, tc.session, tc.pane, nil, now); usage != nil {
				t.Fatalf("got %+v, want nil", usage)
			}
		})
	}
}

func TestResolve_MissingWindowYieldsTokenOnlyUsage(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "s1", PaneID: "w1:p1", ContextTokens: 900, UpdatedAtMs: now})
	usage := ResolveUsageIn(dir, strPtr("s1"), nil, nil, now)
	if usage == nil || usage.ContextTokens != 900 {
		t.Fatalf("got %+v", usage)
	}
	if usage.WindowTokens != nil {
		t.Fatalf("WindowTokens = %v, want nil", usage.WindowTokens)
	}
}

func TestResolve_EmptyDirectory(t *testing.T) {
	if usage := ResolveUsageIn(filepath.Join(t.TempDir(), "absent"), strPtr("s1"), strPtr("w1:p1"), nil, now); usage != nil {
		t.Fatalf("got %+v, want nil", usage)
	}
}
