/**
 * Caches the last Kilo reading next to usage-history.json.
 *
 * Only the resolved ProviderLimits is stored — never the gateway login — so a
 * stale cache file cannot leak a credential. The entry is keyed by the login's
 * hashed identity, so an account switch refuses the previous account's numbers
 * instead of showing them against the new login.
 */
package limits

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	// A success is reused for two minutes. The sidebar collects on every pane
	// status change, so the window has to be long enough to matter and short
	// enough that a Kilo Pass purchase shows up promptly.
	kiloCacheSuccessTTLMs = 120_000
	// A failure is reused for longer: an account without a plan, or a revoked
	// login, must not re-hit the endpoint on every pane event.
	kiloCacheFailureTTLMs = 600_000
)

type kiloCacheOutcome string

const (
	kiloOutcomeFetched kiloCacheOutcome = "fetched"
	kiloOutcomeFailed  kiloCacheOutcome = "failed"
)

type kiloCacheEntry struct {
	FetchedAtMs int64            `json:"fetchedAtMs"`
	AccountID   string           `json:"accountId"`
	Outcome     kiloCacheOutcome `json:"outcome"`
	// Limits is the last reading this account actually produced. A failed
	// attempt leaves it untouched, so the snapshot survives a blip for a later
	// attempt to recover from.
	Limits *ProviderLimits `json:"limits,omitempty"`
	// Failure is the failed attempt's own reading: no window, and the note
	// naming what went wrong. It is what a still-fresh failed entry replays.
	Failure *ProviderLimits `json:"failure,omitempty"`
}

func kiloCachePath() string {
	if v := os.Getenv("USAGEBAR_KILO_CACHE_PATH"); v != "" {
		return v
	}
	return filepath.Join(historyBaseDir(), "kilo-pass.json")
}

func kiloCacheTTL(outcome kiloCacheOutcome) int64 {
	if outcome == kiloOutcomeFetched {
		return kiloCacheSuccessTTLMs
	}
	return kiloCacheFailureTTLMs
}

// kiloCacheFresh requires the entry to name the same account that is signed in
// now. An entry with no account, or a different one, is not fresh no matter how
// recent it is.
func kiloCacheFresh(entry kiloCacheEntry, accountID string, nowMs int64) bool {
	if entry.FetchedAtMs <= 0 || nowMs < entry.FetchedAtMs {
		return false
	}
	if entry.Outcome == "" || entry.AccountID == "" || entry.AccountID != accountID {
		return false
	}
	return nowMs-entry.FetchedAtMs < kiloCacheTTL(entry.Outcome)
}

// cachedKiloLimits replays a still-fresh cache entry for this account.
//
// A failed entry replays its own failure state, never the snapshot it
// preserved. That snapshot is what a later attempt may recover from, not what
// the failure proved: serving it here would put the previous period's window
// back on screen stamped with the failed attempt's time and none of its note,
// and repeated failures would copy it forward indefinitely.
func cachedKiloLimits(entry kiloCacheEntry, accountID string, nowMs int64) (ProviderLimits, bool) {
	if !kiloCacheFresh(entry, accountID, nowMs) {
		return ProviderLimits{}, false
	}
	reading := entry.Limits
	if entry.Outcome == kiloOutcomeFailed {
		// An entry written before failures were recorded separately carries only
		// the snapshot. Refusing it is the safe direction: re-reading costs one
		// request, while serving it would report an unmeasured window as fresh.
		reading = entry.Failure
	}
	if reading == nil {
		return ProviderLimits{}, false
	}
	replay := *reading
	replay.FetchedAtMs = entry.FetchedAtMs
	return replay, true
}

// readKiloCache returns whatever is on disk, fresh or not. The collector needs
// the expired entry as well: a failed fetch records itself here without
// touching the snapshot it did not disprove.
func readKiloCache() (kiloCacheEntry, bool) {
	raw, err := os.ReadFile(kiloCachePath())
	if err != nil {
		return kiloCacheEntry{}, false
	}
	var entry kiloCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return kiloCacheEntry{}, false
	}
	return entry, true
}

func saveKiloCache(entry kiloCacheEntry) {
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	path := kiloCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o600)
}
