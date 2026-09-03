/**
 * Rate-limit collection for Claude.
 * Priority: ~/.claude.json cachedUsageUtilization -> statusLine cache ->
 * another agent's observation of the same account (see windowpool.go).
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
	PromptCachePresent   bool
	PromptCacheExpiresAt *int64
}

// ClaudeLimitsCacheFile is the on-disk statusLine cache payload.
type ClaudeLimitsCacheFile struct {
	FiveHour             *LimitWindow `json:"fiveHour,omitempty"`
	SevenDay             *LimitWindow `json:"sevenDay,omitempty"`
	FetchedAtMs          int64        `json:"fetchedAtMs"`
	PromptCacheExpiresAt *int64       `json:"promptCacheExpiresAt,omitempty"`
}

// CollectClaudeLimitsOptions overrides paths for tests.
type CollectClaudeLimitsOptions struct {
	StatusLineCachePath string
	ClaudeJSONPath      string
}

// ResolveClaudeLimitsCachePath returns the single-default statusLine cache path.
// Multi-account isolation comes from explicit [[claude.profiles]] config, whose
// paths are computed per config_dir; the synthesized default must stay
// byte-identical to the historical location so it resolves the same on the write
// side (statusLine) and the read side (panel/sidebar), which cannot see
// CLAUDE_CONFIG_DIR.
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
	existing := readClaudeLimitsCacheFile(path)
	payload := ClaudeLimitsCacheFile{}
	if existing != nil {
		payload = *existing
	}
	if rateLimits.FiveHour != nil || rateLimits.SevenDay != nil {
		payload.FetchedAtMs = nowMs
		payload.FiveHour = nil
		payload.SevenDay = nil
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
	}
	if rateLimits.PromptCachePresent {
		payload.PromptCacheExpiresAt = rateLimits.PromptCacheExpiresAt
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

// WriteClaudeLimitsCacheGuarded writes the cache when at least one window or a
// prompt_cache object is present, so an empty statusLine payload (e.g. `{}`)
// cannot overwrite a previously valid cache. Returns whether a write happened.
func WriteClaudeLimitsCacheGuarded(rateLimits RateLimitsInput, nowMs int64, path string) (bool, error) {
	if rateLimits.FiveHour == nil && rateLimits.SevenDay == nil && !rateLimits.PromptCachePresent {
		return false, nil
	}
	return true, WriteClaudeLimitsCache(rateLimits, nowMs, path)
}

func readClaudeLimitsCacheFile(path string) *ClaudeLimitsCacheFile {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var parsed ClaudeLimitsCacheFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	return &parsed
}

// ReadPromptCacheExpiresAt returns the recorded Claude prompt_cache expiry.
func ReadPromptCacheExpiresAt(path string) *int64 {
	parsed := readClaudeLimitsCacheFile(path)
	if parsed == nil {
		return nil
	}
	return parsed.PromptCacheExpiresAt
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

// CollectClaudeLimits returns the freshest observation of this account's
// windows. Claude Code's own artifacts are consulted first, but they are
// caches with a timestamp, not live truth: when another agent has observed
// the same account's same window more recently, that reading wins. Provenance
// does not make a snapshot current — only its timestamp does.
func CollectClaudeLimits(nowMs int64, options CollectClaudeLimitsOptions) ProviderLimits {
	statusPath := options.StatusLineCachePath
	if statusPath == "" {
		statusPath = ResolveClaudeLimitsCachePath()
	}
	jsonPath := options.ClaudeJSONPath
	if jsonPath == "" {
		jsonPath = ResolveClaudeJSONPath()
	}

	native := CollectClaudeLimitsFromJSON(nowMs, jsonPath)
	if fromStatusLine := collectFromStatusLineCache(nowMs, statusPath); fromStatusLine != nil {
		if native == nil || fromStatusLine.FetchedAtMs > native.FetchedAtMs {
			native = fromStatusLine
		}
	}
	// The windows belong to the account, so any agent's reading of them
	// counts — including when Claude Code wrote nothing at all.
	account, _ := AccountEmailFromJSONPath(jsonPath)
	if borrowed := borrowWindows("claude", "Claude", account, nowMs); borrowed != nil {
		if native == nil || borrowed.FetchedAtMs > native.FetchedAtMs {
			return *borrowed
		}
	}
	if native != nil {
		return *native
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
