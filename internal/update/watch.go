/**
 * Idle $limit and $cache refresh: one provider collect plus session-cache
 * fan-out every WatchRefreshInterval while the Agent Usage pane is closed.
 *
 * A lock file keeps a single watcher. A second start exits immediately.
 * Ticks are skipped while the pane heartbeat is fresh so the 15s pane
 * collect remains the only clock when the pane is open. The lock mtime is
 * refreshed on every tick, including skipped ones, and throughout a slow
 * collect, so a live watcher is never treated as stale. A watcher whose lock
 * was reclaimed anyway exits instead of running beside its replacement.
 */
package update

import (
	"os"
	"path/filepath"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

const (
	watchLockName  = "limits-watch.lock"
	watchLockStale = 2 * time.Minute
)

func watchLockPath() string {
	if v := os.Getenv("USAGEBAR_WATCH_LOCK_PATH"); v != "" {
		return v
	}
	return filepath.Join(pluginStateDir(), watchLockName)
}

// WatchAlreadyRunning is true when a live watcher holds the lock.
func WatchAlreadyRunning(now time.Time) bool {
	st, err := os.Stat(watchLockPath())
	if err != nil {
		return false
	}
	return now.Sub(st.ModTime()) <= watchLockStale
}

func tryAcquireWatchLock(now time.Time) (*os.File, bool) {
	path := watchLockPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		touchWatchLock(now)
		return f, true
	}
	st, statErr := os.Stat(path)
	if statErr != nil {
		return nil, false
	}
	if now.Sub(st.ModTime()) <= watchLockStale {
		return nil, false
	}
	_ = os.Remove(path)
	f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, false
	}
	touchWatchLock(now)
	return f, true
}

func touchWatchLock(now time.Time) {
	_ = os.Chtimes(watchLockPath(), now, now)
}

// ownsWatchLock is true while the lock path still names the file this
// watcher created; a reclaim replaces it with a different file.
func ownsWatchLock(f *os.File) bool {
	if f == nil {
		return false
	}
	held, err := f.Stat()
	if err != nil {
		return false
	}
	current, err := os.Stat(watchLockPath())
	return err == nil && os.SameFile(held, current)
}

func releaseWatchLock(f *os.File) {
	if f == nil {
		return
	}
	if ownsWatchLock(f) {
		_ = os.Remove(watchLockPath())
	}
	_ = f.Close()
}

// beatWatchLock keeps the lock fresh while a collect outlasts watchLockStale.
func beatWatchLock(f *os.File) func() {
	ticker := time.NewTicker(watchLockStale / 4)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				if ownsWatchLock(f) {
					touchWatchLock(now)
				}
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
	}
}

func collectWatchProviders(cwd *string, nowMs int64) []limits.ProviderLimits {
	opts := limits.DefaultCollectOptions()
	snaps, panesOK := ListOpenPaneSnapshots()
	opts.Only = limits.BillingProviderFilter(snaps, panesOK, limits.DefaultBillingDeps())
	return limits.CollectAllProviderLimits(cwd, nowMs, opts)
}

// ShouldSkipWatchTick is true when the Agent Usage pane is already
// publishing on its 15s loop.
func ShouldSkipWatchTick(now time.Time) bool {
	return PaneHeartbeatFresh(now, heartbeatFreshFor)
}

type watchLoop struct {
	now     func() time.Time
	sleep   func(time.Duration)
	stop    <-chan struct{}
	skip    func(time.Time) bool
	tick    func()
	acquire func(time.Time) (*os.File, bool)
	release func(*os.File)
	touch   func(time.Time)
	owns    func(*os.File) bool
	beat    func(*os.File) func()
}

func (w watchLoop) run() {
	if w.now == nil || w.sleep == nil || w.tick == nil || w.acquire == nil {
		return
	}
	lock, ok := w.acquire(w.now())
	if !ok {
		return
	}
	if w.release != nil {
		defer w.release(lock)
	}
	// runTick reports false once another watcher has reclaimed the lock.
	runTick := func() bool {
		if w.owns != nil && !w.owns(lock) {
			return false
		}
		now := w.now()
		if w.touch != nil {
			w.touch(now)
		}
		if w.skip != nil && w.skip(now) {
			return true
		}
		if w.beat != nil {
			defer w.beat(lock)()
		}
		w.tick()
		return true
	}
	if !runTick() {
		return
	}
	for {
		if stopped(w.stop) {
			return
		}
		w.sleep(WatchRefreshInterval)
		if stopped(w.stop) {
			return
		}
		if !runTick() {
			return
		}
	}
}

func stopped(stop <-chan struct{}) bool {
	if stop == nil {
		return false
	}
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

// RunWatch holds the singleton lock and refreshes idle $limit rows until
// the process is killed. A second instance exits without collecting.
func RunWatch(cwd *string, now func() time.Time, sleep func(time.Duration), stop <-chan struct{}) {
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	watchLoop{
		now:     now,
		sleep:   sleep,
		stop:    stop,
		skip:    ShouldSkipWatchTick,
		acquire: tryAcquireWatchLock,
		release: releaseWatchLock,
		touch:   touchWatchLock,
		owns:    ownsWatchLock,
		beat:    beatWatchLock,
		tick: func() {
			tickNow := now()
			nowMs := tickNow.UnixMilli()
			PublishCollectedLimits(collectWatchProviders(cwd, nowMs), nowMs)
			PublishOpenPaneCaches(tickNow)
		},
	}.run()
}
