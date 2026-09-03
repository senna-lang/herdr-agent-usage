/**
 * Extracts the rate_limits portion from the statusLine stdin JSON passed
 * by Claude Code.
 */
package ratelimit

import "encoding/json"

// RateWindowInput is one rate-limit window from statusLine.
type RateWindowInput struct {
	UsedPercentage float64
	ResetsAt       int64
}

// RateLimitsInput is the parsed rate_limits from Claude Code statusLine.
type RateLimitsInput struct {
	FiveHour *RateWindowInput
	SevenDay *RateWindowInput
}

// PromptCacheInput is Claude Code v2.1.251+ statusLine prompt_cache.
// Present is true only when the object itself was in the payload.
// ExpiresAt is the recorded Unix-seconds expiry; nil when the prefix is cold.
type PromptCacheInput struct {
	Present   bool
	ExpiresAt *int64
}

// ParseRateLimits extracts five_hour / seven_day from statusLine stdin JSON.
// Returns nil only for invalid JSON. Missing rate_limits yields empty windows (not null fields).
func ParseRateLimits(stdinJSON string) *RateLimitsInput {
	var parsed struct {
		RateLimits *struct {
			FiveHour *struct {
				UsedPercentage *float64 `json:"used_percentage"`
				ResetsAt       *float64 `json:"resets_at"`
			} `json:"five_hour"`
			SevenDay *struct {
				UsedPercentage *float64 `json:"used_percentage"`
				ResetsAt       *float64 `json:"resets_at"`
			} `json:"seven_day"`
		} `json:"rate_limits"`
	}
	if err := json.Unmarshal([]byte(stdinJSON), &parsed); err != nil {
		return nil
	}
	toWindow := func(raw *struct {
		UsedPercentage *float64 `json:"used_percentage"`
		ResetsAt       *float64 `json:"resets_at"`
	}) *RateWindowInput {
		if raw == nil || raw.UsedPercentage == nil || raw.ResetsAt == nil {
			return nil
		}
		return &RateWindowInput{
			UsedPercentage: *raw.UsedPercentage,
			ResetsAt:       int64(*raw.ResetsAt),
		}
	}
	out := &RateLimitsInput{}
	if parsed.RateLimits != nil {
		out.FiveHour = toWindow(parsed.RateLimits.FiveHour)
		out.SevenDay = toWindow(parsed.RateLimits.SevenDay)
	}
	return out
}

// ParsePromptCache extracts prompt_cache.expires_at from statusLine stdin.
// Returns nil when the object is absent or the JSON is invalid. A present
// object with null expires_at still returns Present=true so callers can
// hide a stale TTL instead of guessing one.
func ParsePromptCache(stdinJSON string) *PromptCacheInput {
	var parsed struct {
		PromptCache *struct {
			ExpiresAt *float64 `json:"expires_at"`
		} `json:"prompt_cache"`
	}
	if err := json.Unmarshal([]byte(stdinJSON), &parsed); err != nil {
		return nil
	}
	if parsed.PromptCache == nil {
		return nil
	}
	out := &PromptCacheInput{Present: true}
	if parsed.PromptCache.ExpiresAt != nil {
		expires := int64(*parsed.PromptCache.ExpiresAt)
		out.ExpiresAt = &expires
	}
	return out
}
