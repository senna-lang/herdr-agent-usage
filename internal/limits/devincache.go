/**
 * Devin's binding of the shared account-quota cache (quotacache.go).
 *
 * Only the resolved ProviderLimits is stored — never the API key — so a
 * stale cache file cannot leak a credential. The entry is keyed by the
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
	// enough that a quota change shows up promptly.
	devinCacheSuccessTTLMs = 120_000
	// A failure is reused for longer: an unreachable endpoint must not be
	// re-hit on every pane event.
	devinCacheFailureTTLMs = 600_000
)

type devinCacheEntry = quotaCacheEntry

const (
	devinOutcomeFetched = quotaOutcomeFetched
	devinOutcomeFailed  = quotaOutcomeFailed
)

var devinQuotaCache = quotaCache{
	path:         devinCachePath,
	successTTLMs: devinCacheSuccessTTLMs,
	failureTTLMs: devinCacheFailureTTLMs,
}

func devinCachePath() string {
	if v := os.Getenv("USAGEBAR_DEVIN_CACHE_PATH"); v != "" {
		return v
	}
	return filepath.Join(historyBaseDir(), "devin-quota.json")
}

func readDevinCache() (devinCacheEntry, bool) { return devinQuotaCache.read() }

func saveDevinCache(entry devinCacheEntry) { devinQuotaCache.save(entry) }

// cachedDevinLimits replays a still-fresh cache entry for this account; see
// quotaCache.replay for the failure-snapshot semantics.
func cachedDevinLimits(entry devinCacheEntry, accountID string, nowMs int64) (ProviderLimits, bool) {
	return devinQuotaCache.replay(entry, accountID, nowMs)
}
