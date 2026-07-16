/**
 * Rate-limit collection for Claude Code.
 * Priority: ~/.claude.json cachedUsageUtilization -> statusLine cache
 */
package limits

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
)

// RateLimitsInput is the statusLine rate_limits shape.
type RateLimitsInput struct {
	FiveHour *struct {
		UsedPercentage float64
		ResetsAt       int64
	}
	SevenDay *struct {
		UsedPercentage float64
		ResetsAt       int64
	}
}

// ClaudeLimitsCacheFile is the on-disk statusLine cache payload.
type ClaudeLimitsCacheFile struct {
	FiveHour    *LimitWindow `json:"fiveHour,omitempty"`
	SevenDay    *LimitWindow `json:"sevenDay,omitempty"`
	FetchedAtMs int64        `json:"fetchedAtMs"`
}

// CollectClaudeLimitsOptions overrides paths for tests.
type CollectClaudeLimitsOptions struct {
	StatusLineCachePath string
	ClaudeJSONPath      string
}

// ResolveClaudeLimitsCachePath returns the statusLine cache path.
func ResolveClaudeLimitsCachePath() string {
	if v := os.Getenv("USAGEBAR_CLAUDE_LIMITS_PATH"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "herdr-usagebar", "claude-limits-latest.json")
}

// WriteClaudeLimitsCache writes statusLine RateLimitsInput to the cache file.
func WriteClaudeLimitsCache(rateLimits RateLimitsInput, nowMs int64, path string) error {
	if path == "" {
		path = ResolveClaudeLimitsCachePath()
	}
	payload := ClaudeLimitsCacheFile{FetchedAtMs: nowMs}
	if rateLimits.FiveHour != nil {
		wm := 300
		r := rateLimits.FiveHour.ResetsAt
		payload.FiveHour = &LimitWindow{
			UsedPercentage: rateLimits.FiveHour.UsedPercentage,
			ResetsAt:       &r,
			WindowMinutes:  &wm,
		}
	}
	if rateLimits.SevenDay != nil {
		wm := 10080
		r := rateLimits.SevenDay.ResetsAt
		payload.SevenDay = &LimitWindow{
			UsedPercentage: rateLimits.SevenDay.UsedPercentage,
			ResetsAt:       &r,
			WindowMinutes:  &wm,
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func collectFromStatusLineCache(nowMs int64, path string) *ProviderLimits {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var parsed ClaudeLimitsCacheFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	if parsed.FiveHour == nil && parsed.SevenDay == nil {
		return nil
	}
	fetched := parsed.FetchedAtMs
	if fetched == 0 {
		fetched = nowMs
	}
	ageMin := int(math.Max(0, math.Round(float64(nowMs-fetched)/60_000)))
	out := ProviderLimits{
		ProviderID:  "claude",
		Label:       "Claude",
		Primary:     parsed.FiveHour,
		Secondary:   parsed.SevenDay,
		Source:      "claude statusLine cache",
		FetchedAtMs: fetched,
	}
	if ageMin > 30 {
		note := "stale ~" + itoa(ageMin) + "m ago"
		out.Note = &note
	}
	return &out
}

// CollectClaudeLimits prefers claude.json; falls back to the statusLine cache.
func CollectClaudeLimits(nowMs int64, options CollectClaudeLimitsOptions) ProviderLimits {
	statusPath := options.StatusLineCachePath
	if statusPath == "" {
		statusPath = ResolveClaudeLimitsCachePath()
	}
	jsonPath := options.ClaudeJSONPath
	if jsonPath == "" {
		jsonPath = ResolveClaudeJSONPath()
	}

	if fromJSON := CollectClaudeLimitsFromJSON(nowMs, jsonPath); fromJSON != nil {
		return *fromJSON
	}
	if fromCache := collectFromStatusLineCache(nowMs, statusPath); fromCache != nil {
		return *fromCache
	}
	note := "no ~/.claude.json utilization and no statusLine cache"
	return ProviderLimits{
		ProviderID:  "claude",
		Label:       "Claude",
		Source:      "none",
		FetchedAtMs: nowMs,
		Note:        &note,
	}
}
