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
