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
	"strconv"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/fsutil"
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
	// AccountEmail and OrganizationUUID are the login that produced a window
	// write. Both must be set to stamp the cache. A prompt-cache-only update
	// ignores them so it cannot relabel windows it did not rewrite.
	AccountEmail     string
	OrganizationUUID string
}

// ClaudeLimitsCacheFile is the on-disk statusLine cache payload.
type ClaudeLimitsCacheFile struct {
	FiveHour             *LimitWindow `json:"fiveHour,omitempty"`
	SevenDay             *LimitWindow `json:"sevenDay,omitempty"`
	FetchedAtMs          int64        `json:"fetchedAtMs"`
	PromptCacheExpiresAt *int64       `json:"promptCacheExpiresAt,omitempty"`
	AccountEmail         string       `json:"accountEmail,omitempty"`
	OrganizationUUID     string       `json:"organizationUuid,omitempty"`
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
		email := strings.TrimSpace(rateLimits.AccountEmail)
		org := strings.TrimSpace(rateLimits.OrganizationUUID)
		if email != "" && org != "" {
			payload.AccountEmail = email
			payload.OrganizationUUID = org
		} else {
			// A window write that captured no identity must not keep a stale stamp.
			payload.AccountEmail = ""
			payload.OrganizationUUID = ""
		}
	}
	if rateLimits.PromptCachePresent {
		payload.PromptCacheExpiresAt = rateLimits.PromptCacheExpiresAt
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WritePrivateFile(path, append(b, '\n'))
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

// WithClaudeAccountProvenance copies a complete login from claudeJSONPath onto
// input. Email or organizationUuid alone is not an identity: both fields are
// cleared unless both are present. The statusLine bridge calls this so a
// window write can stamp the account that produced it.
func WithClaudeAccountProvenance(input RateLimitsInput, claudeJSONPath string) RateLimitsInput {
	email, org, ok := claudeLoginIdentity(claudeJSONPath)
	if !ok {
		input.AccountEmail = ""
		input.OrganizationUUID = ""
		return input
	}
	input.AccountEmail = email
	input.OrganizationUUID = org
	return input
}

func claudeAccountKey(email, org string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	org = strings.TrimSpace(org)
	if email == "" || org == "" {
		return ""
	}
	// Length prefixes keep arbitrary email and org text unambiguous. The
	// result is an opaque join token, not a display value.
	return strconv.Itoa(len(email)) + ":" + email + "\n" + strconv.Itoa(len(org)) + ":" + org
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
	return readStatusLineSnapshot(nowMs, path).limits
}

// CollectClaudeLimits returns this profile's windows. A statusLine cache is
// current account data only when its stamped identity matches the complete
// login in claudeJSONPath. An untagged cache stays local and is not shared.
// A cache stamped for a different account is ignored. Native utilization in
// ~/.claude.json has no observation account; the current oauthAccount in that
// file does not prove those windows, so they stay local and unjoined. A
// profile with no windows but a complete login still carries the join key, so
// a proven sibling reading can fill it. Borrowed observations without
// organization provenance are shown but not joined.
func CollectClaudeLimits(nowMs int64, options CollectClaudeLimitsOptions) ProviderLimits {
	statusPath := options.StatusLineCachePath
	if statusPath == "" {
		statusPath = ResolveClaudeLimitsCachePath()
	}
	jsonPath := options.ClaudeJSONPath
	if jsonPath == "" {
		jsonPath = ResolveClaudeJSONPath()
	}

	loginEmail, loginOrg, loginComplete := claudeLoginIdentity(jsonPath)
	accountKey := ""
	if loginComplete {
		accountKey = claudeAccountKey(loginEmail, loginOrg)
	}

	var chosen *ProviderLimits
	chosenProven := false
	consider := func(next *ProviderLimits, proven bool) {
		if next == nil {
			return
		}
		if chosen == nil || next.FetchedAtMs > chosen.FetchedAtMs {
			chosen = next
			chosenProven = proven
		}
	}

	// cachedUsageUtilization has no account of its own. The oauthAccount
	// beside it is the login now, not proof of who those windows belong to.
	consider(CollectClaudeLimitsFromJSON(nowMs, jsonPath), false)

	if snap := readStatusLineSnapshot(nowMs, statusPath); snap.limits != nil {
		cacheKey := claudeAccountKey(snap.email, snap.org)
		switch {
		case cacheKey != "" && loginComplete && cacheKey == accountKey:
			consider(snap.limits, true)
		case cacheKey == "":
			consider(snap.limits, false)
		}
	}

	// Borrow matches on email only. That is not organization provenance, so
	// the reading may be shown but must not join another profile.
	borrowEmail, _ := AccountEmailFromJSONPath(jsonPath)
	if borrowed := borrowWindows("claude", "Claude", borrowEmail, nowMs); borrowed != nil {
		consider(borrowed, false)
	}

	if chosen != nil {
		out := *chosen
		if chosenProven {
			out.Account = accountKey
		}
		return out
	}
	note := "no ~/.claude.json utilization and no statusLine cache"
	out := ProviderLimits{
		ProviderID:   "claude",
		Label:        "Claude",
		Source:       "none",
		FetchedAtMs:  nowMs,
		Note:         &note,
		NoReadingYet: true,
	}
	if loginComplete {
		out.Account = accountKey
	}
	return out
}

type statusLineSnapshot struct {
	limits *ProviderLimits
	email  string
	org    string
}

func readStatusLineSnapshot(nowMs int64, path string) statusLineSnapshot {
	parsed := readClaudeLimitsCacheFile(path)
	if parsed == nil || (parsed.FiveHour == nil && parsed.SevenDay == nil) {
		return statusLineSnapshot{}
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
		markStale(&out, ageMin)
	}
	return statusLineSnapshot{limits: &out, email: parsed.AccountEmail, org: parsed.OrganizationUUID}
}
