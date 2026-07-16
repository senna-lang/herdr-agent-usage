/**
 * Persists rate-limit notification state with an O_EXCL lock file under
 * ~/.claude/herdr-usagebar/ (or USAGEBAR_STATE_DIR).
 */
package ratelimit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	lockRetryInterval = 20 * time.Millisecond
	lockTimeout       = 2 * time.Second
	lockStale         = 10 * time.Second
)

func baseDir() string {
	if v := os.Getenv("USAGEBAR_STATE_DIR"); v != "" {
		_ = os.MkdirAll(v, 0o755)
		return v
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".claude", "herdr-usagebar")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func stateFilePath() string {
	return filepath.Join(baseDir(), "rate-limit-state.json")
}

func lockFilePath() string {
	return filepath.Join(baseDir(), "rate-limit-state.lock")
}

func providerStateFilePath() string {
	if v := os.Getenv("USAGEBAR_PROVIDER_NOTIFY_PATH"); v != "" {
		return v
	}
	return filepath.Join(baseDir(), "provider-limit-notify-state.json")
}

// AcquireLock returns true when the lock was acquired.
// On timeout returns false without destroying another process's lock.
func AcquireLock() bool {
	path := lockFilePath()
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return true
		}
		// stale lock?
		if st, err := os.Stat(path); err == nil {
			if time.Since(st.ModTime()) > lockStale {
				_ = os.Remove(path)
				continue
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(lockRetryInterval)
	}
}

// ReleaseLock removes the lock file.
func ReleaseLock() {
	_ = os.Remove(lockFilePath())
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

func readClaudeState() ClaudeNotifyState {
	raw, err := os.ReadFile(stateFilePath())
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

func writeClaudeState(state ClaudeNotifyState) {
	wire := map[string]any{
		"fiveHour": toWire(state.FiveHour),
		"sevenDay": toWire(state.SevenDay),
	}
	path := stateFilePath()
	tmp := path + ".tmp"
	b, err := json.Marshal(wire)
	if err != nil {
		return
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// WithLockedState runs read→update→write under lock for Claude statusLine state.
func WithLockedState(update func(current ClaudeNotifyState) ClaudeNotifyState) ClaudeNotifyState {
	locked := AcquireLock()
	defer func() {
		if locked {
			ReleaseLock()
		}
	}()
	current := readClaudeState()
	next := update(current)
	if locked {
		writeClaudeState(next)
	}
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
	tmp := path + ".tmp"
	b, err := json.Marshal(wire)
	if err != nil {
		return
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// WithLockedProviderState runs provider-primary notify under the same lock.
func WithLockedProviderState(update func(current ProviderNotifyStateMap) ProviderNotifyStateMap) ProviderNotifyStateMap {
	locked := AcquireLock()
	defer func() {
		if locked {
			ReleaseLock()
		}
	}()
	current := readProviderState()
	next := update(current)
	if locked {
		writeProviderState(next)
	}
	return next
}
