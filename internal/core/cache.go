/**
 * Session prompt-cache diagnostics for the sidebar.
 *
 * Hit rate is session-cumulative: read / (fresh + read + creation).
 * Remaining TTL is published only from a recorded expiry: an absolute
 * expires_at, or a recorded bucket duration plus last cache-bearing
 * activity. Missing evidence leaves TTL blank rather than guessing 5m/1h.
 */
package core

import (
	"fmt"
	"math"
)

// CacheUsage is the prompt-cache observation for one session.
// Counters are already aggregated by the provider; this type only carries
// the display-ready result.
type CacheUsage struct {
	FreshInputTokens int
	ReadTokens       int
	CreationTokens   int
	HitPercent       float64
	// TTLSeconds is a recorded bucket duration (e.g. Anthropic cacheWrite1h → 3600).
	// It is not a remaining countdown by itself.
	TTLSeconds *int64
	// LastActivityUnix is the Unix-seconds timestamp of the latest cache-bearing turn.
	LastActivityUnix *int64
	// ExpiresAtUnix is an absolute expiry (Claude Code prompt_cache.expires_at).
	// Preferred over TTLSeconds + LastActivityUnix when present.
	ExpiresAtUnix *int64
}

// CacheFromTokenCounts builds CacheUsage from non-negative token counters.
// Returns nil when the session recorded no cache-bearing input.
func CacheFromTokenCounts(fresh, read, creation int) *CacheUsage {
	if fresh < 0 {
		fresh = 0
	}
	if read < 0 {
		read = 0
	}
	if creation < 0 {
		creation = 0
	}
	total := fresh + read + creation
	if total == 0 {
		return nil
	}
	return &CacheUsage{
		FreshInputTokens: fresh,
		ReadTokens:       read,
		CreationTokens:   creation,
		HitPercent:       float64(read) / float64(total) * 100,
	}
}

// RemainingTTLSeconds returns remaining cache lifetime in seconds.
// Zero means the recorded expiry has already passed; nil means there is
// no recorded expiry to count down from.
func RemainingTTLSeconds(cache CacheUsage, nowUnix int64) *int64 {
	if cache.ExpiresAtUnix != nil {
		remaining := *cache.ExpiresAtUnix - nowUnix
		if remaining < 0 {
			remaining = 0
		}
		return &remaining
	}
	if cache.TTLSeconds == nil || cache.LastActivityUnix == nil {
		return nil
	}
	remaining := *cache.LastActivityUnix + *cache.TTLSeconds - nowUnix
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}

// FormatCacheStatus renders the sidebar cache row.
// A recorded remaining TTL of 0 (prefix already cold) prefixes ⚠️.
// Unknown TTL is not a warning: missing expiry is not evidence of a miss.
func FormatCacheStatus(cache CacheUsage, nowUnix int64) string {
	hit := fmt.Sprintf("cache reuse %.1f%%", cache.HitPercent)
	remaining := RemainingTTLSeconds(cache, nowUnix)
	if remaining == nil {
		return hit
	}
	if *remaining <= 0 {
		return "⚠️ " + hit
	}
	return hit + " · ttl≈" + formatCacheTTL(*remaining)
}

func formatCacheTTL(seconds int64) string {
	if seconds >= 3600 {
		hours := int(math.Round(float64(seconds) / 3600))
		if hours < 1 {
			hours = 1
		}
		return fmt.Sprintf("%dh", hours)
	}
	minutes := int(math.Round(float64(seconds) / 60))
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf("%dm", minutes)
}
