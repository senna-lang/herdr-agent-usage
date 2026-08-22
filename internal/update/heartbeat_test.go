/**
 * Tests for the Agent Usage pane heartbeat used to back off the idle watcher.
 */
package update

import (
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
