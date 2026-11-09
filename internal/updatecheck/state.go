package updatecheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/fsutil"
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
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return fsutil.WritePrivateFile(statePath(dir), raw)
}

// acquireLock lets simultaneous focus events coalesce to one check. A stale
// lock from a killed process is discarded after two minutes.
func acquireLock(dir string, now time.Time) (func(), bool) {
	if err := fsutil.MkdirPrivate(dir); err != nil {
		return nil, false
	}
	path := lockPath(dir)
	f, err := fsutil.OpenPrivateExclusive(path)
	if err != nil {
		if info, statErr := os.Stat(path); statErr == nil && now.Sub(info.ModTime()) > 2*time.Minute {
			_ = os.Remove(path)
			f, err = fsutil.OpenPrivateExclusive(path)
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
