/**
 * Tests for the idle $limit watcher lock and skip/tick loop.
 */
package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func isolateWatch(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USAGEBAR_STATE_DIR", dir)
	t.Setenv("USAGEBAR_WATCH_LOCK_PATH", filepath.Join(dir, watchLockName))
	t.Setenv("USAGEBAR_PANE_HEARTBEAT_PATH", filepath.Join(dir, heartbeatFileName))
}

func TestTryAcquireWatchLock_SecondInstanceLoses(t *testing.T) {
	isolateWatch(t)
	now := time.UnixMilli(1_700_000_000_000)
	first, ok := tryAcquireWatchLock(now)
	if !ok {
		t.Fatal("first acquire must succeed")
	}
	defer releaseWatchLock(first)
	if _, ok := tryAcquireWatchLock(now.Add(time.Second)); ok {
		t.Fatal("second live watcher must lose")
	}
}

func TestWatchAlreadyRunning(t *testing.T) {
	isolateWatch(t)
	now := time.UnixMilli(1_700_000_000_000)
	if WatchAlreadyRunning(now) {
		t.Fatal("missing lock is not running")
	}
	first, ok := tryAcquireWatchLock(now)
	if !ok {
		t.Fatal("acquire")
	}
	defer releaseWatchLock(first)
	if !WatchAlreadyRunning(now.Add(time.Second)) {
		t.Fatal("live lock must look running")
	}
	if WatchAlreadyRunning(now.Add(watchLockStale + time.Second)) {
		t.Fatal("stale lock must not look running")
	}
}

func TestTryAcquireWatchLock_StaleLockIsReclaimed(t *testing.T) {
	isolateWatch(t)
	now := time.UnixMilli(1_700_000_000_000)
	if err := os.WriteFile(watchLockPath(), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := now.Add(-watchLockStale - time.Second)
	if err := os.Chtimes(watchLockPath(), stale, stale); err != nil {
		t.Fatal(err)
	}
	lock, ok := tryAcquireWatchLock(now)
	if !ok {
		t.Fatal("stale lock must be reclaimed")
	}
	releaseWatchLock(lock)
}

func TestWatchLoop_SecondAcquireDoesNotTick(t *testing.T) {
	ticks := 0
	watchLoop{
		now:   func() time.Time { return time.UnixMilli(1) },
		sleep: func(time.Duration) {},
		stop:  alreadyStopped(),
		tick:  func() { ticks++ },
		acquire: func(time.Time) (*os.File, bool) {
			return nil, false
		},
	}.run()
	if ticks != 0 {
		t.Fatalf("ticks=%d", ticks)
	}
}

func TestWatchLoop_SkipsThenTicks(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	ticks := 0
	skips := 0
	sleeps := 0
	stop := make(chan struct{})
	watchLoop{
		now: func() time.Time { return now },
		sleep: func(d time.Duration) {
			if d != WatchRefreshInterval {
				t.Fatalf("slept %s", d)
			}
			sleeps++
			now = now.Add(d)
		},
		stop: stop,
		skip: func(time.Time) bool {
			skips++
			return skips == 1
		},
		tick: func() {
			ticks++
			close(stop)
		},
		acquire: func(time.Time) (*os.File, bool) { return nil, true },
		release: func(*os.File) {},
		touch:   func(time.Time) {},
	}.run()
	if ticks != 1 || skips != 2 || sleeps != 1 {
		t.Fatalf("ticks=%d skips=%d sleeps=%d", ticks, skips, sleeps)
	}
}

func alreadyStopped() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
