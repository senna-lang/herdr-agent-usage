/**
 * Caches the last Devin quota reading next to usage-history.json.
 *
 * Only the resolved ProviderLimits is stored — never the API key — so a
 * stale cache file cannot leak a credential. The entry is keyed by the
 * login's hashed identity, so an account switch refuses the previous
 * account's numbers instead of showing them against the new login.
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
	// enough that a quota change shows up promptly.
	devinCacheSuccessTTLMs = 120_000
	// A failure is reused for longer: an unreachable endpoint must not be
	// re-hit on every pane event.
	devinCacheFailureTTLMs = 600_000
)

type devinCacheOutcome string

const (
	devinOutcomeFetched devinCacheOutcome = "fetched"
	devinOutcomeFailed  devinCacheOutcome = "failed"
)

type devinCacheEntry struct {
	FetchedAtMs int64             `json:"fetchedAtMs"`
	AccountID   string            `json:"accountId"`
	Outcome     devinCacheOutcome `json:"outcome"`
	// Limits is the last reading this account actually produced. A failed
	// attempt leaves it untouched, so the snapshot survives a blip for a
	// later attempt to recover from.
	Limits *ProviderLimits `json:"limits,omitempty"`
	// Failure is the failed attempt's own reading: no window, and the note
	// naming what went wrong. It is what a still-fresh failed entry replays.
	Failure *ProviderLimits `json:"failure,omitempty"`
}

func devinCachePath() string {
	if v := os.Getenv("USAGEBAR_DEVIN_CACHE_PATH"); v != "" {
		return v
	}
	return filepath.Join(historyBaseDir(), "devin-quota.json")
}

func devinCacheTTL(outcome devinCacheOutcome) int64 {
	if outcome == devinOutcomeFetched {
		return devinCacheSuccessTTLMs
	}
	return devinCacheFailureTTLMs
}

// devinCacheFresh requires the entry to name the same account that is signed
// in now. An entry with no account, or a different one, is not fresh no
// matter how recent it is.
func devinCacheFresh(entry devinCacheEntry, accountID string, nowMs int64) bool {
	if entry.FetchedAtMs <= 0 || nowMs < entry.FetchedAtMs {
		return false
	}
	if entry.Outcome == "" || entry.AccountID == "" || entry.AccountID != accountID {
		return false
	}
	return nowMs-entry.FetchedAtMs < devinCacheTTL(entry.Outcome)
}

// cachedDevinLimits replays a still-fresh cache entry for this account.
//
// A failed entry replays its own failure state, never the snapshot it
// preserved. That snapshot is what a later attempt may recover from, not
// what the failure proved: serving it here would put the previous period's
// window back on screen stamped with the failed attempt's time and none of
// its note.
func cachedDevinLimits(entry devinCacheEntry, accountID string, nowMs int64) (ProviderLimits, bool) {
	if !devinCacheFresh(entry, accountID, nowMs) {
		return ProviderLimits{}, false
	}
	reading := entry.Limits
	if entry.Outcome == devinOutcomeFailed {
		reading = entry.Failure
	}
	if reading == nil {
		return ProviderLimits{}, false
	}
	replay := *reading
	replay.FetchedAtMs = entry.FetchedAtMs
	return replay, true
}

// readDevinCache returns whatever is on disk, fresh or not. The collector
// needs the expired entry as well: a failed fetch records itself here
// without touching the snapshot it did not disprove.
func readDevinCache() (devinCacheEntry, bool) {
	raw, err := os.ReadFile(devinCachePath())
	if err != nil {
		return devinCacheEntry{}, false
	}
	var entry devinCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return devinCacheEntry{}, false
	}
	return entry, true
}

func saveDevinCache(entry devinCacheEntry) {
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	path := devinCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o600)
}
