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

// CollectCodexLimits prefers cwd rollout; falls back to newest rollout.
func CollectCodexLimits(cwd *string, nowMs int64) ProviderLimits {
	var path string
	if cwd != nil && *cwd != "" {
		path = codex.FindLatestSessionFileForCwd(*cwd)
	}
	if path == "" {
		newest := ListNewestRolloutPaths(1)
		if len(newest) > 0 {
			path = newest[0]
		}
	}
	if path == "" {
		note := "no rollout jsonl under ~/.codex/sessions"
		return ProviderLimits{ProviderID: "codex", Label: "Codex", Source: "none", FetchedAtMs: nowMs, Note: &note}
	}

	lines, err := fsutil.ReadLastNLines(path, codexTailScanBytes)
	if err != nil {
		note := "read failed: " + err.Error()
		return ProviderLimits{ProviderID: "codex", Label: "Codex", Source: path, FetchedAtMs: nowMs, Note: &note}
	}
	extracted := ExtractRateLimitsFromLines(lines)
	if extracted == nil {
		note := "rollout found but no rate_limits in recent token_count"
		return ProviderLimits{ProviderID: "codex", Label: "Codex", Source: path, FetchedAtMs: nowMs, Note: &note}
	}
	plan := planlabels.CodexPlanLabel(extracted.PlanType)
	return ProviderLimits{
		ProviderID:  "codex",
		Label:       "Codex",
		Primary:     extracted.Primary,
		Secondary:   extracted.Secondary,
		PlanType:    plan,
		Source:      "codex rollout",
		FetchedAtMs: nowMs,
	}
}
