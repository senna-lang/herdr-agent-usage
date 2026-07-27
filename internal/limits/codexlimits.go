/**
 * Reads rate_limits (primary/secondary) from a Codex rollout jsonl.
 */
package limits

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/fsutil"
	"github.com/senna-lang/herdr-agent-usage/internal/planlabels"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
)

const codexTailScanBytes = 512 * 1024

// codexMaxRolloutsToScan bounds how many rollouts (newest-first) we read looking
// for a rate_limits snapshot. The freshest snapshot lives in the most recently
// active session, so the first file almost always has it; the cap only guards
// the pathological case of many just-opened sessions with no token_count yet.
const codexMaxRolloutsToScan = 25

// codexStaleAfterMinutes matches the statusLine cache tolerance in
// claudecache.go. Past it the snapshot is labeled rather than trusted
// silently — a rollout is only as current as the turn that wrote it.
const codexStaleAfterMinutes = 30

// ExtractedCodexRateLimits is the raw extract result (planType still raw).
type ExtractedCodexRateLimits struct {
	Primary   *LimitWindow
	Secondary *LimitWindow
	PlanType  *string
}

// ExtractRateLimitsFromLines extracts the most recent rate_limits from lines.
func ExtractRateLimitsFromLines(lines []string) *ExtractedCodexRateLimits {
	for i := len(lines) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(lines[i])
		if raw == "" {
			continue
		}
		var parsed struct {
			Type    string `json:"type"`
			Payload *struct {
				Type       string `json:"type"`
				RateLimits *struct {
					Primary   *rateWindowRaw `json:"primary"`
					Secondary *rateWindowRaw `json:"secondary"`
					PlanType  *string        `json:"plan_type"`
				} `json:"rate_limits"`
				Info *struct {
					RateLimits *struct {
						Primary   *rateWindowRaw `json:"primary"`
						Secondary *rateWindowRaw `json:"secondary"`
						PlanType  *string        `json:"plan_type"`
					} `json:"rate_limits"`
				} `json:"info"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if parsed.Type != "event_msg" || parsed.Payload == nil || parsed.Payload.Type != "token_count" {
			continue
		}
		rate := parsed.Payload.RateLimits
		if rate == nil && parsed.Payload.Info != nil {
			rate = parsed.Payload.Info.RateLimits
		}
		if rate == nil {
			continue
		}
		primary := toWindow(rate.Primary)
		secondary := toWindow(rate.Secondary)
		if primary == nil && secondary == nil {
			continue
		}
		return &ExtractedCodexRateLimits{
			Primary: primary, Secondary: secondary, PlanType: rate.PlanType,
		}
	}
	return nil
}

type rateWindowRaw struct {
	UsedPercent   *float64 `json:"used_percent"`
	WindowMinutes *float64 `json:"window_minutes"`
	ResetsAt      *float64 `json:"resets_at"`
}

func toWindow(raw *rateWindowRaw) *LimitWindow {
	if raw == nil || raw.UsedPercent == nil || !isFiniteF(*raw.UsedPercent) {
		return nil
	}
	w := &LimitWindow{UsedPercentage: *raw.UsedPercent}
	if raw.ResetsAt != nil && isFiniteF(*raw.ResetsAt) {
		r := int64(*raw.ResetsAt)
		w.ResetsAt = &r
	}
	if raw.WindowMinutes != nil && isFiniteF(*raw.WindowMinutes) {
		m := int(*raw.WindowMinutes)
		w.WindowMinutes = &m
	}
	return w
}

func isFiniteF(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0)
}

// ListNewestRolloutPaths returns up to max rollout paths by mtime desc.
func ListNewestRolloutPaths(max int) []string {
	root := filepath.Join(codexHome(), "sessions")
	var matches []struct {
		path    string
		mtimeMs int64
	}
	years, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, year := range years {
		if !year.IsDir() {
			continue
		}
		yearPath := filepath.Join(root, year.Name())
		months, err := os.ReadDir(yearPath)
		if err != nil {
			continue
		}
		for _, month := range months {
			if !month.IsDir() {
				continue
			}
			monthPath := filepath.Join(yearPath, month.Name())
			days, err := os.ReadDir(monthPath)
			if err != nil {
				continue
			}
			for _, day := range days {
				if !day.IsDir() {
					continue
				}
				dayPath := filepath.Join(monthPath, day.Name())
				files, err := os.ReadDir(dayPath)
				if err != nil {
					continue
				}
				for _, name := range files {
					n := name.Name()
					if !strings.HasPrefix(n, "rollout-") || !strings.HasSuffix(n, ".jsonl") {
						continue
					}
					full := filepath.Join(dayPath, n)
					st, err := os.Stat(full)
					if err != nil || !st.Mode().IsRegular() {
						continue
					}
					matches = append(matches, struct {
						path    string
						mtimeMs int64
					}{full, st.ModTime().UnixMilli()})
				}
			}
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].mtimeMs > matches[j].mtimeMs })
	if max < 0 {
		max = 0
	}
	if max > len(matches) {
		max = len(matches)
	}
	out := make([]string, max)
	for i := 0; i < max; i++ {
		out[i] = matches[i].path
	}
	return out
}

func codexHome() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// CollectCodexLimits reads the account-global Codex rate-limit windows from the
// freshest rollout snapshot across ALL sessions.
//
// Codex rate limits are account-wide: every session records the same
// primary/secondary window (same resets_at/window_minutes), only snapshotted at
// its own last token_count. Resolving per pane cwd (as this once did) made two
// panes with different cwds show snapshots captured at different times — the
// less-recently-active pane displaying a stale, lower %. So cwd is intentionally
// ignored here (unlike the per-pane sidebar context meter, a separate path),
// matching the Claude/OpenCode/Grok collectors. Assumes a single Codex account
// (~/.codex is single-auth); with multiple accounts this reports whichever
// session turned most recently.
//
// A rollout snapshot is a cache with a timestamp, not live truth. When
// another agent has observed this same account's windows more recently, that
// reading wins (see windowpool.go) — which is also what makes a Codex
// subscription driven through another harness report real numbers at all.
func CollectCodexLimits(_ *string, nowMs int64) ProviderLimits {
	paths := ListNewestRolloutPaths(codexMaxRolloutsToScan)
	// Newest-first: take the first rollout that carries a rate_limits snapshot.
	// A just-opened session has session_meta but no token_count yet, so the
	// newest file is not always the one with data.
	for _, path := range paths {
		lines, err := fsutil.ReadLastNLines(path, codexTailScanBytes)
		if err != nil {
			continue
		}
		extracted := ExtractRateLimitsFromLines(lines)
		if extracted == nil {
			continue
		}
		observedMs := rolloutObservedAtMs(path)
		out := ProviderLimits{
			ProviderID:  "codex",
			Label:       "Codex",
			Primary:     extracted.Primary,
			Secondary:   extracted.Secondary,
			PlanType:    planlabels.CodexPlanLabel(extracted.PlanType),
			Source:      "codex rollout",
			FetchedAtMs: nowMs,
		}
		if age := minutesBetween(observedMs, nowMs); age > codexStaleAfterMinutes {
			note := "stale ~" + itoa(age) + "m ago"
			out.Note = &note
		}
		if borrowed := borrowCodexWindows(nowMs); borrowed != nil && borrowed.FetchedAtMs > observedMs {
			return *borrowed
		}
		return out
	}
	if borrowed := borrowCodexWindows(nowMs); borrowed != nil {
		return *borrowed
	}
	if len(paths) == 0 {
		note := "no rollout jsonl under ~/.codex/sessions"
		return ProviderLimits{ProviderID: "codex", Label: "Codex", Source: "none", FetchedAtMs: nowMs, Note: &note}
	}
	note := "rollout found but no rate_limits in recent token_count"
	return ProviderLimits{ProviderID: "codex", Label: "Codex", Source: "codex rollout", FetchedAtMs: nowMs, Note: &note}
}

// borrowCodexWindows takes another agent's observation of this Codex account.
// Signed in locally? Then the observation must belong to that same account.
// Signed out, there is no identity to contradict, and the pool's own
// single-account rule is what keeps the borrow honest.
func borrowCodexWindows(nowMs int64) *ProviderLimits {
	return borrowWindows("codex", "Codex", codex.AccountID(), nowMs)
}

// rolloutObservedAtMs is when the rollout last recorded a turn, i.e. when its
// rate-limit snapshot was taken. 0 when the file cannot be stat'd.
func rolloutObservedAtMs(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixMilli()
}

// minutesBetween is whole elapsed minutes, never negative.
func minutesBetween(fromMs, toMs int64) int {
	if fromMs <= 0 || toMs <= fromMs {
		return 0
	}
	return int((toMs - fromMs) / 60_000)
}
