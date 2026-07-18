package updatecheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	stateFileName = "update-check-state.json"
	lockFileName  = "update-check.lock"
)

// CheckInterval is the minimum interval between automatic checks.
const CheckInterval = 24 * time.Hour

// State records when GitHub was last contacted and which release has already
// produced a toast.
type State struct {
	LastCheckedAt         time.Time `json:"lastCheckedAt"`
	LatestNotifiedVersion string    `json:"latestNotifiedVersion,omitempty"`
}

func statePath(dir string) string { return filepath.Join(dir, stateFileName) }

func lockPath(dir string) string { return filepath.Join(dir, lockFileName) }

func readState(dir string) State {
	raw, err := os.ReadFile(statePath(dir))
	if err != nil {
		return State{}
	}
	var state State
	if json.Unmarshal(raw, &state) != nil {
		return State{}
	}
	return state
}

func writeState(dir string, state State) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, statePath(dir))
}

// acquireLock lets simultaneous focus events coalesce to one check. A stale
// lock from a killed process is discarded after two minutes.
func acquireLock(dir string, now time.Time) (func(), bool) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false
	}
	path := lockPath(dir)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if info, statErr := os.Stat(path); statErr == nil && now.Sub(info.ModTime()) > 2*time.Minute {
			_ = os.Remove(path)
			f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		}
	}
	if err != nil {
		return nil, false
	}
	_ = f.Close()
	return func() { _ = os.Remove(path) }, true
}

func shouldCheck(state State, now time.Time, force bool) bool {
	return force || state.LastCheckedAt.IsZero() || now.Sub(state.LastCheckedAt) >= CheckInterval
}
