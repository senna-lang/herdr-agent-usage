/**
 * Tests for WithLockedState / acquireLock — the flock must exclude concurrent
 * holders, never depend on file mtime, and never remove the lock file.
 */
package ratelimit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	oldLockName  = "rate-limit-state.lock"
	flockName    = "rate-limit-state.flock"
	deadHolderAt = -3 * time.Minute
)

func withTempStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USAGEBAR_STATE_DIR", dir)
	return dir
}

// writeAged creates an empty file whose mtime is offset from now.
func writeAged(t *testing.T, path string, offset time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(offset)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// holdLock takes the lock for dir and releases it on cleanup (idempotently,
// so a test may release earlier).
func holdLock(t *testing.T, dir string) (release func()) {
	t.Helper()
	rel, ok := acquireLock(dir)
	if !ok {
		t.Fatal("expected lock")
	}
	var once sync.Once
	release = func() { once.Do(rel) }
	t.Cleanup(release)
	return release
}

func TestAcquireLock_Fresh(t *testing.T) {
	dir := withTempStateDir(t)
	release, ok := acquireLock("")
	if !ok {
		t.Fatal("expected lock")
	}
	if _, err := os.Stat(filepath.Join(dir, flockName)); err != nil {
		t.Fatal(err)
	}
	release()
}

func TestAcquireLock_ReleaseKeepsFile(t *testing.T) {
	dir := withTempStateDir(t)
	release, ok := acquireLock(dir)
	if !ok {
		t.Fatal("expected lock")
	}
	release()
	if _, err := os.Stat(filepath.Join(dir, flockName)); err != nil {
		t.Fatalf("lock file must persist after release: %v", err)
	}
	release2, ok := acquireLock(dir)
	if !ok {
		t.Fatal("expected re-acquire after release")
	}
	release2()
}

// A file left behind by a dead holder is not locked by the kernel, so it must
// not block, whatever its age and whichever protocol created it.
func TestAcquireLock_LeftoverFilesFromDeadHolderDoNotBlock(t *testing.T) {
	dir := withTempStateDir(t)
	writeAged(t, filepath.Join(dir, oldLockName), deadHolderAt)
	writeAged(t, filepath.Join(dir, flockName), deadHolderAt)

	start := time.Now()
	release, ok := acquireLock(dir)
	if !ok {
		t.Fatal("leftover lock files must not block")
	}
	defer release()
	if took := time.Since(start); took > lockTimeout/4 {
		t.Fatalf("acquire took %v, must not wait for the timeout", took)
	}
}

// mtime plays no part: an old-looking file that is actually flocked stays held.
func TestAcquireLock_DoesNotTakeOverHeldLockWithOldMtime(t *testing.T) {
	dir := withTempStateDir(t)
	release := holdLock(t, dir)
	lockPath := filepath.Join(dir, flockName)
	old := time.Now().Add(deadHolderAt)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if rel, ok := acquireLock(dir); ok {
		rel()
		t.Fatal("must not take over a held lock")
	}
	if took := time.Since(start); took < lockTimeout {
		t.Fatalf("gave up after %v, want >= %v", took, lockTimeout)
	}

	release()
	rel, ok := acquireLock(dir)
	if !ok {
		t.Fatal("expected acquire after release")
	}
	rel()
}

func TestWithLockedState_Writes(t *testing.T) {
	dir := withTempStateDir(t)
	b50 := Bucket50
	next := WithLockedState(func(current ClaudeNotifyState) ClaudeNotifyState {
		return ClaudeNotifyState{
			FiveHour: &WindowState{ResetsAt: 100, NotifiedBucket: &b50, FailedNotifyAttempts: 0},
			SevenDay: nil,
		}
	})
	if next.FiveHour == nil || next.FiveHour.NotifiedBucket == nil || *next.FiveHour.NotifiedBucket != Bucket50 {
		t.Fatalf("%+v", next)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "rate-limit-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	_ = json.Unmarshal(raw, &onDisk)
	five := onDisk["fiveHour"].(map[string]any)
	if five["notifiedBucket"] != "50" {
		t.Fatalf("%v", five)
	}
	// Released: the lock can be taken again immediately.
	release, ok := acquireLock(dir)
	if !ok {
		t.Fatal("lock should be released")
	}
	release()
}

// TestWithLockedState_SkipsUpdateWhenLocked prevents a concurrent statusline
// render from showing a duplicate toast without being able to persist its
// deduplication state.
func TestWithLockedState_SkipsUpdateWhenLocked(t *testing.T) {
	dir := withTempStateDir(t)
	statePath := filepath.Join(dir, "rate-limit-state.json")
	existing := []byte(`{"fiveHour":{"resetsAt":1,"notifiedBucket":"20","failedNotifyAttempts":0},"sevenDay":null}`)
	if err := os.WriteFile(statePath, existing, 0o644); err != nil {
		t.Fatal(err)
	}
	holdLock(t, dir)

	called := false
	next := WithLockedState(func(current ClaudeNotifyState) ClaudeNotifyState {
		called = true
		return ClaudeNotifyState{}
	})
	if called {
		t.Fatal("update must not run without the persistence lock")
	}
	if next.FiveHour == nil || next.FiveHour.NotifiedBucket == nil || *next.FiveHour.NotifiedBucket != Bucket20 {
		t.Fatalf("got %+v, want persisted state", next)
	}
	onDisk, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(existing) {
		t.Fatalf("disk changed: %s", onDisk)
	}
	if _, err := os.Stat(filepath.Join(dir, flockName)); err != nil {
		t.Fatal("other lock must remain")
	}
}

func TestWithLockedProviderState_SkipsUpdateWhenLocked(t *testing.T) {
	dir := withTempStateDir(t)
	statePath := filepath.Join(dir, "provider-limit-notify-state.json")
	existing := []byte(`{"codex":{"resetsAt":1,"notifiedBucket":"20","failedNotifyAttempts":0}}`)
	if err := os.WriteFile(statePath, existing, 0o644); err != nil {
		t.Fatal(err)
	}
	holdLock(t, dir)

	called := false
	next := WithLockedProviderState(func(current ProviderNotifyStateMap) ProviderNotifyStateMap {
		called = true
		return ProviderNotifyStateMap{}
	})
	if called {
		t.Fatal("update must not run without the persistence lock")
	}
	if next["codex"] == nil || next["codex"].NotifiedBucket == nil || *next["codex"].NotifiedBucket != Bucket20 {
		t.Fatalf("got %+v, want persisted state", next)
	}
}

// TestRunRateLimitCheck_LeftoverLocksShowOneToast is the regression for the
// O_EXCL+mtime protocol: with leftover 3-minute-old lock files, concurrent
// statusline runs could each reclaim the lock and all show the same toast.
func TestRunRateLimitCheck_LeftoverLocksShowOneToast(t *testing.T) {
	const (
		trials     = 20
		goroutines = 32
		nowMs      = int64(1_700_000_000_000)
		stdin      = `{"rate_limits":{"five_hour":{"used_percentage":55,"resets_at":1800000000}}}`
	)
	for trial := range trials {
		dir := t.TempDir()
		writeAged(t, filepath.Join(dir, oldLockName), deadHolderAt)
		writeAged(t, filepath.Join(dir, flockName), deadHolderAt)

		var toasts, holders, maxHolders atomic.Int32
		notify := func(title, body string) bool {
			toasts.Add(1)
			cur := holders.Add(1)
			for {
				prev := maxHolders.Load()
				if cur <= prev || maxHolders.CompareAndSwap(prev, cur) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			holders.Add(-1)
			return true
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		for range goroutines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				RunRateLimitCheckWithThresholdsIn(dir, stdin, nowMs, nil, notify, "")
			}()
		}
		close(start)
		wg.Wait()

		if got := toasts.Load(); got != 1 {
			t.Fatalf("trial %d: toasts = %d, want 1", trial, got)
		}
		if got := maxHolders.Load(); got != 1 {
			t.Fatalf("trial %d: max concurrent holders = %d, want 1", trial, got)
		}
	}
}
