/**
 * Persists rate-limit notification state under ~/.claude/herdr-usagebar/ (or
 * USAGEBAR_STATE_DIR), guarded by an advisory flock on a lock file that is
 * never removed. The flock gives mutual exclusion across processes and across
 * separate opens within one process, and the kernel drops it when the holder
 * exits, so a crashed run can never leave the state locked.
 */
package ratelimit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/senna-lang/herdr-agent-usage/internal/fsutil"
)

const (
	lockRetryInterval = 20 * time.Millisecond
	lockTimeout       = 2 * time.Second
)

// baseDir is the single-default notify-state dir. Per-profile isolation is
// threaded explicitly via the *In variants (resolveDir); the default here stays
// byte-identical to the historical location so it resolves the same regardless
// of whether CLAUDE_CONFIG_DIR is visible (it is not on the read side).
func baseDir() string {
	if v := os.Getenv("USAGEBAR_STATE_DIR"); v != "" {
		_ = fsutil.MkdirPrivate(v)
		return v
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".claude", "herdr-usagebar")
	_ = fsutil.MkdirPrivate(dir)
	return dir
}

// resolveDir returns an explicit per-profile state dir, or the env/default
// baseDir() when empty. The multi-profile write path passes the resolved
// profile's StateDir so two accounts never share notify state or lock.
func resolveDir(dir string) string {
	if dir == "" {
		return baseDir()
	}
	_ = fsutil.MkdirPrivate(dir)
	return dir
}

func stateFilePathIn(dir string) string {
	return filepath.Join(resolveDir(dir), "rate-limit-state.json")
}

// lockFilePathIn is deliberately not the old O_EXCL `rate-limit-state.lock`:
// an older usagebar still running during an upgrade unlinks that name on
// release, which would otherwise remove the file a new process holds a flock
// on. A leftover old `.lock` file is simply ignored.
func lockFilePathIn(dir string) string {
	return filepath.Join(resolveDir(dir), "rate-limit-state.flock")
}

func providerStateFilePathIn(dir string) string {
	if v := os.Getenv("USAGEBAR_PROVIDER_NOTIFY_PATH"); v != "" {
		return v
	}
	return filepath.Join(resolveDir(dir), "provider-limit-notify-state.json")
}

func providerStateFilePath() string { return providerStateFilePathIn("") }

// acquireLock takes the exclusive notify-state flock in dir ("" = default
// dir, as resolveDir) and returns its release func. On timeout or any open or
// flock error it returns ok=false and holds nothing.
//
// The lock file is never removed, not even by release: removing a path that
// another process holds a flock on would let a third process create and lock
// a new inode at the same path, so two holders would coexist. Because the
// kernel releases the flock when the holder exits, there is no staleness or
// reclaim logic.
func acquireLock(dir string) (release func(), ok bool) {
	path := lockFilePathIn(dir)
	if err := fsutil.MkdirPrivate(filepath.Dir(path)); err != nil {
		return nil, false
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fsutil.PrivateFileMode)
	if err != nil {
		return nil, false
	}
	fd := int(f.Fd())
	deadline := time.Now().Add(lockTimeout)
	for {
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				_ = unix.Flock(fd, unix.LOCK_UN)
				_ = f.Close()
			}, true
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			_ = f.Close()
			return nil, false
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, false
		}
		time.Sleep(lockRetryInterval)
	}
}

// windowStateWire is the on-disk JSON shape for WindowState.
type windowStateWire struct {
	ResetsAt             int64   `json:"resetsAt"`
	NotifiedBucket       *string `json:"notifiedBucket"`
	FailedNotifyAttempts int     `json:"failedNotifyAttempts"`
}

func toWire(ws *WindowState) *windowStateWire {
	if ws == nil {
		return nil
	}
	w := &windowStateWire{
		ResetsAt:             ws.ResetsAt,
		FailedNotifyAttempts: ws.FailedNotifyAttempts,
	}
	if ws.NotifiedBucket != nil {
		s := string(*ws.NotifiedBucket)
		w.NotifiedBucket = &s
	}
	return w
}

func fromWire(w *windowStateWire) *WindowState {
	if w == nil {
		return nil
	}
	ws := &WindowState{
		ResetsAt:             w.ResetsAt,
		FailedNotifyAttempts: w.FailedNotifyAttempts,
	}
	if w.NotifiedBucket != nil {
		b := Bucket(*w.NotifiedBucket)
		ws.NotifiedBucket = &b
	}
	return ws
}

// NotifyState is Claude five-hour / seven-day window state.
type ClaudeNotifyState struct {
	FiveHour *WindowState `json:"fiveHour"`
	SevenDay *WindowState `json:"sevenDay"`
}

func readClaudeStateIn(dir string) ClaudeNotifyState {
	raw, err := os.ReadFile(stateFilePathIn(dir))
	if err != nil {
		return ClaudeNotifyState{}
	}
	var wire struct {
		FiveHour *windowStateWire `json:"fiveHour"`
		SevenDay *windowStateWire `json:"sevenDay"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return ClaudeNotifyState{}
	}
	return ClaudeNotifyState{
		FiveHour: fromWire(wire.FiveHour),
		SevenDay: fromWire(wire.SevenDay),
	}
}

func writeClaudeStateIn(dir string, state ClaudeNotifyState) {
	wire := map[string]any{
		"fiveHour": toWire(state.FiveHour),
		"sevenDay": toWire(state.SevenDay),
	}
	path := stateFilePathIn(dir)
	b, err := json.Marshal(wire)
	if err != nil {
		return
	}
	_ = fsutil.WritePrivateFile(path, b)
}

// WithLockedState runs read→update→write under lock for Claude statusLine state.
func WithLockedState(update func(current ClaudeNotifyState) ClaudeNotifyState) ClaudeNotifyState {
	return WithLockedStateIn("", update)
}

// WithLockedStateIn is WithLockedState scoped to an explicit state dir. It
// never runs update without the lock, since that decision could not persist.
func WithLockedStateIn(dir string, update func(current ClaudeNotifyState) ClaudeNotifyState) ClaudeNotifyState {
	release, ok := acquireLock(dir)
	if !ok {
		return readClaudeStateIn(dir)
	}
	defer release()

	current := readClaudeStateIn(dir)
	next := update(current)
	writeClaudeStateIn(dir, next)
	return next
}

// ProviderNotifyStateMap is keyed by provider id.
type ProviderNotifyStateMap map[string]*WindowState

func readProviderState() ProviderNotifyStateMap {
	raw, err := os.ReadFile(providerStateFilePath())
	if err != nil {
		return ProviderNotifyStateMap{}
	}
	var wire map[string]*windowStateWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return ProviderNotifyStateMap{}
	}
	out := ProviderNotifyStateMap{}
	for k, v := range wire {
		out[k] = fromWire(v)
	}
	return out
}

func writeProviderState(state ProviderNotifyStateMap) {
	wire := map[string]any{}
	for k, v := range state {
		wire[k] = toWire(v)
	}
	path := providerStateFilePath()
	b, err := json.Marshal(wire)
	if err != nil {
		return
	}
	_ = fsutil.WritePrivateFile(path, b)
}

// WithLockedProviderState runs provider-primary notify under the same lock.
// It does not run update without the lock because its deduplication result
// would not be persisted.
func WithLockedProviderState(update func(current ProviderNotifyStateMap) ProviderNotifyStateMap) ProviderNotifyStateMap {
	release, ok := acquireLock("")
	if !ok {
		return readProviderState()
	}
	defer release()

	current := readProviderState()
	next := update(current)
	writeProviderState(next)
	return next
}
