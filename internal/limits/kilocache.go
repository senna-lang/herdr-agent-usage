/**
 * Kilo's binding of the shared account-quota cache (quotacache.go).
 *
 * Only the resolved ProviderLimits is stored — never the gateway login — so
 * a stale cache file cannot leak a credential. The entry is keyed by the
 * login's hashed identity, so an account switch refuses the previous
 * account's numbers instead of showing them against the new login.
 */
package limits

import (
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

type kiloCacheEntry = quotaCacheEntry
type kiloCacheOutcome = quotaCacheOutcome

const (
	kiloOutcomeFetched = quotaOutcomeFetched
	kiloOutcomeFailed  = quotaOutcomeFailed
)

var kiloQuotaCache = quotaCache{
	path:         kiloCachePath,
	successTTLMs: kiloCacheSuccessTTLMs,
	failureTTLMs: kiloCacheFailureTTLMs,
}

func kiloCachePath() string {
	if v := os.Getenv("USAGEBAR_KILO_CACHE_PATH"); v != "" {
		return v
	}
	return filepath.Join(historyBaseDir(), "kilo-pass.json")
}

func readKiloCache() (kiloCacheEntry, bool) { return kiloQuotaCache.read() }

func saveKiloCache(entry kiloCacheEntry) { kiloQuotaCache.save(entry) }

// cachedKiloLimits replays a still-fresh cache entry for this account; see
// quotaCache.replay for the failure-snapshot semantics.
func cachedKiloLimits(entry kiloCacheEntry, accountID string, nowMs int64) (ProviderLimits, bool) {
	return kiloQuotaCache.replay(entry, accountID, nowMs)
}
