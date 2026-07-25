/**
 * TTL rules for the last opencode.ai usage fetch.
 *
 * The sidebar collects OpenCode limits on every pane agent_status_changed, so
 * an uncached web path would mean two HTTP requests (and a browser cookie
 * import) per event. Unsuccessful attempts are cached too, but the back-off
 * depends on why they failed: a real fetch failure waits out a long TTL, while
 * "no session yet" only pauses briefly so signing in to opencode.ai takes
 * effect on the next refresh instead of minutes later. Disk access lives in
 * opencodewebcache_io.go.
 */
package limits

// openCodeWebOutcome is why the last attempt ended the way it did.
type openCodeWebOutcome string

const (
	// openCodeWebFetched means opencode.ai returned usage.
	openCodeWebFetched openCodeWebOutcome = "fetched"
	// openCodeWebFetchFailed means a session was available but the fetch or
	// parse failed (expired cookie, changed page, network error).
	openCodeWebFetchFailed openCodeWebOutcome = "fetch-failed"
	// openCodeWebNoSession means no cookie header could be resolved at all.
	openCodeWebNoSession openCodeWebOutcome = "no-session"
)

const (
	openCodeWebCacheSuccessTTLMs   = 120_000
	openCodeWebCacheFailureTTLMs   = 600_000
	openCodeWebCacheNoSessionTTLMs = 30_000
)

// openCodeWebCacheEntry is the persisted result of the last web attempt.
// Limits is nil unless Outcome is openCodeWebFetched, and a fresh entry with
// nil Limits means "skip the network this cycle and use the local estimate".
type openCodeWebCacheEntry struct {
	FetchedAtMs int64              `json:"fetchedAtMs"`
	Outcome     openCodeWebOutcome `json:"outcome"`
	Limits      *ProviderLimits    `json:"limits,omitempty"`
}

// openCodeWebCacheTTLMs is how long an outcome suppresses another attempt.
func openCodeWebCacheTTLMs(outcome openCodeWebOutcome) int64 {
	switch outcome {
	case openCodeWebFetched:
		return openCodeWebCacheSuccessTTLMs
	case openCodeWebNoSession:
		return openCodeWebCacheNoSessionTTLMs
	default:
		return openCodeWebCacheFailureTTLMs
	}
}

// openCodeWebCacheFresh reports whether an entry may still be reused. Clock
// jumps backwards invalidate rather than pin a stale entry into the future.
func openCodeWebCacheFresh(entry openCodeWebCacheEntry, nowMs int64) bool {
	if entry.FetchedAtMs <= 0 || nowMs < entry.FetchedAtMs || entry.Outcome == "" {
		return false
	}
	if entry.Outcome == openCodeWebFetched && entry.Limits == nil {
		return false
	}
	return nowMs-entry.FetchedAtMs < openCodeWebCacheTTLMs(entry.Outcome)
}
