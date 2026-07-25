/**
 * Persists the last opencode.ai usage fetch next to usage-history.json.
 * Only the resolved ProviderLimits is stored — never the cookie header — so a
 * stale cache file can never leak a browser session.
 */
package limits

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func openCodeWebCachePath() string {
	if v := os.Getenv("USAGEBAR_OPENCODE_WEB_CACHE_PATH"); v != "" {
		return v
	}
	return filepath.Join(historyBaseDir(), "opencode-go-web.json")
}

// loadOpenCodeWebCache returns the cached limits when the last attempt is
// still fresh. fresh=true with nil limits means a cached unsuccessful attempt:
// skip the network and fall through to the local estimate.
func loadOpenCodeWebCache(nowMs int64) (limits *ProviderLimits, fresh bool) {
	raw, err := os.ReadFile(openCodeWebCachePath())
	if err != nil {
		return nil, false
	}
	var entry openCodeWebCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, false
	}
	if !openCodeWebCacheFresh(entry, nowMs) {
		return nil, false
	}
	return entry.Limits, true
}

// saveOpenCodeWebCache records the outcome of an attempt. limits is nil for
// every outcome other than openCodeWebFetched.
func saveOpenCodeWebCache(limits *ProviderLimits, outcome openCodeWebOutcome, nowMs int64) {
	entry := openCodeWebCacheEntry{FetchedAtMs: nowMs, Outcome: outcome, Limits: limits}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	path := openCodeWebCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o600)
}
