/**
 * Tests for Antigravity context resolution: session identity, pane fallback,
 * freshness, and the cases that must resolve to no usage at all.
 */
package antigravity

import (
	"path/filepath"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
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
		ContextTokens: 18558, WindowTokens: windowOf(1048576), UpdatedAtMs: now,
	})
	usage := ResolveUsageIn(dir, strPtr("live"), strPtr("w1:p1"), strPtr("/repo"), now)
	if usage == nil {
		t.Fatal("got nil, want usage")
	}
	if usage.ContextTokens != 18558 || usage.WindowTokens == nil || *usage.WindowTokens != 1048576 {
		t.Fatalf("got %+v", usage)
	}
}

// After a cleared/resumed conversation, Antigravity writes snapshots under a
// new conversation id while herdr keeps reporting the one it saw at launch.
// Pane identity must still resolve it.
func TestResolve_ClearedSessionResolvesByPane(t *testing.T) {
	dir := seed(t, Snapshot{
		SessionID: "new", PaneID: "w1:p1", Cwd: "/repo",
		ContextTokens: 100, UpdatedAtMs: now,
	})
	usage := ResolveUsageIn(dir, strPtr("stale-reported-id"), strPtr("w1:p1"), strPtr("/repo"), now)
	if usage == nil {
		t.Fatal("got nil, want usage from the pane fallback")
	}
	if usage.ContextTokens != 100 {
		t.Fatalf("got %+v", usage)
	}
}

// The reported session's snapshot usually still exists after a clear and is
// recent enough to pass the freshness bound, so recency on the pane must beat
// the reported identity.
func TestResolve_ClearedSessionSupersedesTheReportedOne(t *testing.T) {
	dir := seed(t,
		Snapshot{SessionID: "old", PaneID: "w1:p1", ContextTokens: 999, UpdatedAtMs: now - 1000},
		Snapshot{SessionID: "new", PaneID: "w1:p1", ContextTokens: 1, UpdatedAtMs: now},
	)
	usage := ResolveUsageIn(dir, strPtr("old"), strPtr("w1:p1"), nil, now)
	if usage == nil {
		t.Fatal("got nil, want usage")
	}
	if usage.ContextTokens != 1 {
		t.Fatalf("got %+v, want the newer pane snapshot to win", usage)
	}
}

// Supersession needs a pane to compare within: with no pane id the reported
// session stays the best available answer rather than resolving to nothing.
func TestResolve_ReportedSessionKeptWhenPaneUnknown(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "live", ContextTokens: 42, UpdatedAtMs: now})
	usage := ResolveUsageIn(dir, strPtr("live"), nil, nil, now)
	if usage == nil || usage.ContextTokens != 42 {
		t.Fatalf("got %+v", usage)
	}
}

// A newer snapshot belonging to a different pane must not displace this one.
func TestResolve_SupersessionIsScopedToTheSamePane(t *testing.T) {
	dir := seed(t,
		Snapshot{SessionID: "mine", PaneID: "w1:p1", ContextTokens: 5, UpdatedAtMs: now - 1000},
		Snapshot{SessionID: "other", PaneID: "w1:p2", ContextTokens: 999, UpdatedAtMs: now},
	)
	usage := ResolveUsageIn(dir, strPtr("mine"), strPtr("w1:p1"), nil, now)
	if usage == nil || usage.ContextTokens != 5 {
		t.Fatalf("got %+v, want the reported session unaffected by another pane", usage)
	}
}

// Two Antigravity panes in one repository must never borrow each other's
// context. cwd alone cannot tell them apart, which is why resolution is
// pane-keyed.
func TestResolve_TwoPanesSharingCwdDoNotCrossAttribute(t *testing.T) {
	dir := seed(t,
		Snapshot{SessionID: "a", PaneID: "w1:p1", Cwd: "/repo", ContextTokens: 10, UpdatedAtMs: now},
		Snapshot{SessionID: "b", PaneID: "w1:p2", Cwd: "/repo", ContextTokens: 20, UpdatedAtMs: now},
	)
	usage1 := ResolveUsageIn(dir, strPtr("unreported"), strPtr("w1:p1"), strPtr("/repo"), now)
	if usage1 == nil || usage1.ContextTokens != 10 {
		t.Fatalf("pane 1 got %+v", usage1)
	}
	usage2 := ResolveUsageIn(dir, strPtr("unreported"), strPtr("w1:p2"), strPtr("/repo"), now)
	if usage2 == nil || usage2.ContextTokens != 20 {
		t.Fatalf("pane 2 got %+v", usage2)
	}
}

func TestResolve_StaleReturnsNoUsage(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "live", ContextTokens: 1, UpdatedAtMs: now - SnapshotFreshnessMs - 1})
	if usage := ResolveUsageIn(dir, strPtr("live"), nil, nil, now); usage != nil {
		t.Fatalf("got %+v, want nil", usage)
	}
}

func TestResolve_FreshnessBoundaryIsInclusive(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "live", ContextTokens: 1, UpdatedAtMs: now - SnapshotFreshnessMs})
	if usage := ResolveUsageIn(dir, strPtr("live"), nil, nil, now); usage == nil {
		t.Fatal("got nil, want usage exactly at the freshness bound")
	}
}

// A timestamp in the future indicates a clock the resolver cannot reason
// about; it must not be treated as the freshest possible snapshot.
func TestResolve_FutureTimestampIsNotFresh(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "live", ContextTokens: 1, UpdatedAtMs: now + 1})
	if usage := ResolveUsageIn(dir, strPtr("live"), nil, nil, now); usage != nil {
		t.Fatalf("got %+v, want nil", usage)
	}
}

// Two sessions claiming one pane at the same instant give no basis to prefer
// either, so neither is shown.
func TestResolve_AmbiguousPaneMatchReturnsNoUsage(t *testing.T) {
	dir := seed(t,
		Snapshot{SessionID: "a", PaneID: "w1:p1", ContextTokens: 1, UpdatedAtMs: now},
		Snapshot{SessionID: "b", PaneID: "w1:p1", ContextTokens: 2, UpdatedAtMs: now},
	)
	if usage := ResolveUsageIn(dir, strPtr("unreported"), strPtr("w1:p1"), nil, now); usage != nil {
		t.Fatalf("got %+v, want nil", usage)
	}
}

// A snapshot left by a previous occupant of a reused pane id is rejected when
// its working directory contradicts the pane's.
func TestResolve_PaneFallbackRequiresConsistentCwd(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "old-occupant", PaneID: "w1:p1", Cwd: "/other-repo", ContextTokens: 1, UpdatedAtMs: now})
	if usage := ResolveUsageIn(dir, strPtr("unreported"), strPtr("w1:p1"), strPtr("/repo"), now); usage != nil {
		t.Fatalf("got %+v, want nil", usage)
	}
}

func TestResolve_NoIdentifiersReturnsNoUsage(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "live", ContextTokens: 1, UpdatedAtMs: now})
	if usage := ResolveUsageIn(dir, nil, nil, nil, now); usage != nil {
		t.Fatalf("got %+v, want nil", usage)
	}
}

func TestResolve_MissingWindowYieldsTokenOnlyUsage(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "live", ContextTokens: 500, UpdatedAtMs: now})
	usage := ResolveUsageIn(dir, strPtr("live"), nil, nil, now)
	if usage == nil || usage.ContextTokens != 500 || usage.WindowTokens != nil {
		t.Fatalf("got %+v", usage)
	}
}

func TestResolve_EmptyDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if usage := ResolveUsageIn(dir, strPtr("anything"), strPtr("w1:p1"), nil, now); usage != nil {
		t.Fatalf("got %+v, want nil", usage)
	}
}

// The Cache field must survive resolution end to end, since $cache_* reads
// through the same ContextUsage this function returns.
func TestResolve_CachePassesThrough(t *testing.T) {
	dir := seed(t, Snapshot{
		SessionID: "live", ContextTokens: 100, UpdatedAtMs: now,
		Cache: core.CacheFromTokenCounts(10, 20, 5),
	})
	usage := ResolveUsageIn(dir, strPtr("live"), nil, nil, now)
	if usage == nil || usage.Cache == nil {
		t.Fatalf("got %+v, want a populated Cache", usage)
	}
	if usage.Cache.FreshInputTokens != 10 || usage.Cache.ReadTokens != 20 || usage.Cache.CreationTokens != 5 {
		t.Fatalf("Cache = %+v", usage.Cache)
	}
}

func TestLatestFreshSnapshot_PicksNewestAcrossSessions(t *testing.T) {
	dir := seed(t,
		Snapshot{SessionID: "a", PlanTier: "old", UpdatedAtMs: now - 1000},
		Snapshot{SessionID: "b", PlanTier: "new", UpdatedAtMs: now},
	)
	snap, ok := LatestFreshSnapshot(dir, now)
	if !ok || snap.PlanTier != "new" {
		t.Fatalf("got %+v, ok=%v", snap, ok)
	}
}

func TestLatestFreshSnapshot_IgnoresStale(t *testing.T) {
	dir := seed(t, Snapshot{SessionID: "a", UpdatedAtMs: now - SnapshotFreshnessMs - 1})
	if _, ok := LatestFreshSnapshot(dir, now); ok {
		t.Fatal("got ok=true, want a stale-only directory to yield nothing")
	}
}

func TestLatestFreshSnapshot_EmptyDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if _, ok := LatestFreshSnapshot(dir, now); ok {
		t.Fatal("got ok=true, want nothing from an empty directory")
	}
}
