/**
 * Prompt-cache diagnostics for the sidebar.
 *
 * Hit rate is session-cumulative: read / (fresh + read + creation).
 * Remaining TTL is published only from a recorded expiry. A remaining TTL
 * of 0 prefixes ⚠️. Sidebar tokens are plain text: Herdr colors $cache_high /
 * $cache_mid / $cache_low from config.toml, not from ANSI in the value.
 */
package core

import (
	"fmt"
	"math"
)

// CacheUsage is one prompt-cache observation.
// Counters are already aggregated by the provider; this type only carries
// the display-ready result. The same type is used for a single turn and
// for a session-cumulative segment.
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

// FormatCacheStatus renders the sidebar cache row from session-cumulative
// hit rate. A recorded remaining TTL of 0 (prefix already cold) prefixes ⚠️.
// Unknown TTL is not a warning: missing expiry is not evidence of a miss.
// The string is plain text; Herdr metadata tokens do not interpret ANSI.
func FormatCacheStatus(cache CacheUsage, nowUnix int64) string {
	hit := fmt.Sprintf("cache hit %.1f%%", cache.HitPercent)
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

// CacheHitBand is which styled sidebar token should carry the hit-rate row.
type CacheHitBand string

const (
	CacheHitHigh CacheHitBand = "cache_high"
	CacheHitMid  CacheHitBand = "cache_mid"
	CacheHitLow  CacheHitBand = "cache_low"
)

// CacheHitTokenNames is the exclusive set written for one pane's cache row.
var CacheHitTokenNames = []string{string(CacheHitHigh), string(CacheHitMid), string(CacheHitLow)}

// CacheHitBandFor maps session hit rate onto the styled token.
// ≥80% high/green, ≥50% mid/yellow, otherwise low/red.
func CacheHitBandFor(hit float64) CacheHitBand {
	if hit >= 80 {
		return CacheHitHigh
	}
	if hit >= 50 {
		return CacheHitMid
	}
	return CacheHitLow
}

// SidebarCache prefers session-cumulative counters for the hit rate and
// overlays recorded TTL from the latest observation when present.
func SidebarCache(usage ContextUsage) *CacheUsage {
	src := usage.SessionCache
	if src == nil {
		src = usage.Cache
	}
	if src == nil {
		return nil
	}
	out := *src
	if usage.Cache == nil {
		return &out
	}
	if usage.Cache.ExpiresAtUnix != nil {
		out.ExpiresAtUnix = usage.Cache.ExpiresAtUnix
	}
	if usage.Cache.TTLSeconds != nil {
		out.TTLSeconds = usage.Cache.TTLSeconds
	}
	if usage.Cache.LastActivityUnix != nil {
		out.LastActivityUnix = usage.Cache.LastActivityUnix
	}
	return &out
}
