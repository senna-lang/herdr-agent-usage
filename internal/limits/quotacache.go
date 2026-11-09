/**
 * Account-keyed cache of one provider's last quota reading, shared by every
 * collector that owns a remote quota endpoint (Kilo, Devin).
 *
 * The semantics are provider-neutral: only the resolved ProviderLimits is
 * stored — never a credential — so a stale cache file cannot leak one. The
 * entry is keyed by the credential's hashed identity, so an account switch
 * refuses the previous account's numbers instead of showing them against
 * the new login. A failed attempt is remembered under its own TTL so it
 * does not re-hit the endpoint on every pane event, while the last good
 * snapshot is preserved beside the failure rather than in place of it.
 *
 * Each provider binds these semantics to its own cache path and TTLs
 * (devincache.go, kilocache.go); credential handling and API fetching stay
 * in the collector.
 */
package limits

import (
	"encoding/json"
	"os"

	"github.com/senna-lang/herdr-agent-usage/internal/fsutil"
)

type quotaCacheOutcome string

const (
	quotaOutcomeFetched quotaCacheOutcome = "fetched"
	quotaOutcomeFailed  quotaCacheOutcome = "failed"
)

type quotaCacheEntry struct {
	FetchedAtMs int64             `json:"fetchedAtMs"`
	AccountID   string            `json:"accountId"`
	Outcome     quotaCacheOutcome `json:"outcome"`
	// Limits is the last reading this account actually produced. A failed
	// attempt leaves it untouched, so the snapshot survives a blip for a
	// later attempt to recover from.
	Limits *ProviderLimits `json:"limits,omitempty"`
	// Failure is the failed attempt's own reading: no window, and the note
	// naming what went wrong. It is what a still-fresh failed entry replays.
	Failure *ProviderLimits `json:"failure,omitempty"`
}

// quotaCache binds the shared semantics to one provider's cache path and
// per-outcome TTLs.
type quotaCache struct {
	path         func() string
	successTTLMs int64
	failureTTLMs int64
}

func (c quotaCache) ttl(outcome quotaCacheOutcome) int64 {
	if outcome == quotaOutcomeFetched {
		return c.successTTLMs
	}
	return c.failureTTLMs
}

// fresh requires the entry to name the same account that is signed in now.
// An entry with no account, or a different one, is not fresh no matter how
// recent it is.
func (c quotaCache) fresh(entry quotaCacheEntry, accountID string, nowMs int64) bool {
	if entry.FetchedAtMs <= 0 || nowMs < entry.FetchedAtMs {
		return false
	}
	if entry.Outcome == "" || entry.AccountID == "" || entry.AccountID != accountID {
		return false
	}
	return nowMs-entry.FetchedAtMs < c.ttl(entry.Outcome)
}

// replay returns a still-fresh cache entry for this account.
//
// A failed entry replays its own failure state, never the snapshot it
// preserved. That snapshot is what a later attempt may recover from, not
// what the failure proved: serving it here would put the previous period's
// window back on screen stamped with the failed attempt's time and none of
// its note, and repeated failures would copy it forward indefinitely.
func (c quotaCache) replay(entry quotaCacheEntry, accountID string, nowMs int64) (ProviderLimits, bool) {
	if !c.fresh(entry, accountID, nowMs) {
		return ProviderLimits{}, false
	}
	reading := entry.Limits
	if entry.Outcome == quotaOutcomeFailed {
		// An entry written before failures were recorded separately carries
		// only the snapshot. Refusing it is the safe direction: re-reading
		// costs one request, while serving it would report an unmeasured
		// window as fresh.
		reading = entry.Failure
	}
	if reading == nil {
		return ProviderLimits{}, false
	}
	replay := *reading
	replay.FetchedAtMs = entry.FetchedAtMs
	return replay, true
}

// read returns whatever is on disk, fresh or not. The collector needs the
// expired entry as well: a failed fetch records itself here without
// touching the snapshot it did not disprove.
func (c quotaCache) read() (quotaCacheEntry, bool) {
	raw, err := os.ReadFile(c.path())
	if err != nil {
		return quotaCacheEntry{}, false
	}
	var entry quotaCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return quotaCacheEntry{}, false
	}
	return entry, true
}

func (c quotaCache) save(entry quotaCacheEntry) {
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = fsutil.WritePrivateFile(c.path(), raw)
}

// preservedQuotaSnapshot is the last good reading for this same identity,
// and nothing else: an entry measured under a different login must never
// carry its numbers into this one.
func preservedQuotaSnapshot(previous quotaCacheEntry, accountID string) *ProviderLimits {
	if previous.AccountID != accountID || previous.Limits == nil {
		return nil
	}
	return previous.Limits
}
