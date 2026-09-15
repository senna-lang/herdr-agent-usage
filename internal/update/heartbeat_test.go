/**
 * Tests for the Agent Usage pane heartbeat used to back off the idle watcher.
 */
package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func isolateHeartbeat(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USAGEBAR_STATE_DIR", dir)
	t.Setenv("USAGEBAR_PANE_HEARTBEAT_PATH", filepath.Join(dir, heartbeatFileName))
}

func TestPaneHeartbeatFresh_MissingIsStale(t *testing.T) {
	isolateHeartbeat(t)
	if PaneHeartbeatFresh(time.UnixMilli(1_700_000_000_000), heartbeatFreshFor) {
		t.Fatal("missing heartbeat must not look fresh")
	}
}

func TestPaneHeartbeatFresh_WithinWindow(t *testing.T) {
	isolateHeartbeat(t)
	now := time.UnixMilli(1_700_000_000_000)
	TouchPaneHeartbeat(now)
	if !PaneHeartbeatFresh(now.Add(15*time.Second), heartbeatFreshFor) {
		t.Fatal("15s-old heartbeat must still be fresh")
	}
}

func TestPaneHeartbeatFresh_Expired(t *testing.T) {
	isolateHeartbeat(t)
	now := time.UnixMilli(1_700_000_000_000)
	TouchPaneHeartbeat(now)
	if PaneHeartbeatFresh(now.Add(21*time.Second), heartbeatFreshFor) {
		t.Fatal("heartbeat older than 20s must be stale")
	}
}

func TestShouldSkipWatchTick_FollowsHeartbeat(t *testing.T) {
	isolateHeartbeat(t)
	now := time.UnixMilli(1_700_000_000_000)
	if ShouldSkipWatchTick(now) {
		t.Fatal("no heartbeat: watcher must collect")
	}
	TouchPaneHeartbeat(now)
	if !ShouldSkipWatchTick(now) {
		t.Fatal("fresh pane collect: watcher must skip")
	}
}

// A pane process left running across a rebuild ticks a heartbeat using
// whatever build it started with. A reader on a different (newer) build
// must not treat that heartbeat as evidence its own publish logic ran,
// even though the timestamp is within the freshness window.
func TestPaneHeartbeatFresh_DifferentBuildIsStale(t *testing.T) {
	isolateHeartbeat(t)
	now := time.UnixMilli(1_700_000_000_000)
	path := paneHeartbeatPath()
	touchPaneHeartbeatWith(path, now, "build-A")
	if paneHeartbeatFreshWith(path, now.Add(5*time.Second), heartbeatFreshFor, "build-B") {
		t.Fatal("heartbeat from a different build must not look fresh")
	}
	if !paneHeartbeatFreshWith(path, now.Add(5*time.Second), heartbeatFreshFor, "build-A") {
		t.Fatal("heartbeat from the same build must still look fresh")
	}
}

// A pre-fix binary wrote a bare timestamp with no fingerprint at all. That
// legacy format must never count as fresh, matching the real incident: an
// old process kept ticking a heartbeat the current watcher had no way to
// distrust.
func TestPaneHeartbeatFresh_LegacyFormatIsStale(t *testing.T) {
	isolateHeartbeat(t)
	now := time.UnixMilli(1_700_000_000_000)
	path := paneHeartbeatPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("1700000000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if PaneHeartbeatFresh(now.Add(time.Second), heartbeatFreshFor) {
		t.Fatal("legacy unfingerprinted heartbeat must not look fresh")
	}
}
