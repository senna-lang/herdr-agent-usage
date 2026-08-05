/**
 * Renders a ContextUsage as a short sidebar display string.
 * It does not own model tables or token aggregation rules (those are the
 * provider's responsibility).
 *
 * When the sidebar is narrow, we drop elements from least to most important:
 *   1. the icon
 *   2. the absolute token count (Nk)
 *   3. fall back to just the percentage
 */
package core

import (
	"fmt"
	"math"
)

const warningThresholdPercent = 80

// FormatUsageOptions controls sidebar formatting.
type FormatUsageOptions struct {
	// MaxColumns is the maximum display width usable by the context token.
	// When nil, returns the full representation.
	MaxColumns *int
}

// DisplayWidth approximates terminal cell width. Emoji and Misc Symbols count as 2;
// variation selectors count as 0. This is not a precise Unicode East Asian
// Width implementation, but it is sufficient for the symbols used here.
func DisplayWidth(text string) int {
	width := 0
	for _, r := range text {
		cp := int(r)
		if cp <= 0x1f || (cp >= 0x7f && cp <= 0x9f) {
			continue
		}
		// VARIATION SELECTOR-15/16
		if cp == 0xfe0e || cp == 0xfe0f {
			continue
		}
		// combining marks
		if cp >= 0x300 && cp <= 0x36f {
			continue
		}
		// Misc Symbols / Dingbats / many emoji
		if (cp >= 0x2600 && cp <= 0x27bf) ||
			(cp >= 0x1f300 && cp <= 0x1faff) ||
			(cp >= 0x1f900 && cp <= 0x1f9ff) {
			width += 2
			continue
		}
		width++
	}
	return width
}

func iconFor(percent *int) string {
	if percent != nil && *percent >= warningThresholdPercent {
		return "⚠️"
	}
	return "⛁"
}

func formatTokenCount(tokens int) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	thousands := float64(tokens) / 1000
	if thousands < 10 {
		return fmt.Sprintf("%.1fk", thousands)
	}
	return fmt.Sprintf("%.0fk", thousands)
}

// FormatTokenCount formats a token count for compact sidebar display ("94k", "5.0k").
func FormatTokenCount(tokens int) string {
	return formatTokenCount(tokens)
}

// Absolute context-size thresholds for sidebar coloring (Herdr styles each
// tier token with a fixed fg in config; only one tier is non-empty at a time).
const (
	ContextSoftTokens     = 100_000 // light yellow
	ContextWarnTokens     = 150_000 // deep yellow
	ContextCriticalTokens = 180_000 // red
)

// Context tier metadata token names written to the sidebar.
const (
	ContextTokenOK       = "ctx"    // < 100k (default muted style)
	ContextTokenSoft     = "ctx_y"  // ≥ 100k
	ContextTokenWarn     = "ctx_yy" // ≥ 150k
	ContextTokenCritical = "ctx_r"  // ≥ 180k
)

// AllContextTierTokenNames is the exclusive set of context-count tokens.
var AllContextTierTokenNames = []string{
	ContextTokenOK,
	ContextTokenSoft,
	ContextTokenWarn,
	ContextTokenCritical,
}

// ContextTierTokenName picks which exclusive sidebar token should carry the
// compact context count for the given absolute token total. Empty when there
// is nothing to show.
func ContextTierTokenName(contextTokens int) string {
	if contextTokens <= 0 {
		return ""
	}
	switch {
	case contextTokens >= ContextCriticalTokens:
		return ContextTokenCritical
	case contextTokens >= ContextWarnTokens:
		return ContextTokenWarn
	case contextTokens >= ContextSoftTokens:
		return ContextTokenSoft
	default:
		return ContextTokenOK
	}
}

// FormatCompactContextTokens is the shortest useful context meter: disk icon
// plus absolute token count (no window %). Herdr joins adjacent sidebar
// tokens with a space, so no leading padding is needed ("7d 77% ⛁ 94k").
func FormatCompactContextTokens(usage ContextUsage) string {
	if usage.ContextTokens <= 0 {
		return ""
	}
	return "⛁ " + formatTokenCount(usage.ContextTokens)
}

// ContextTierTokenValues returns the exclusive tier map for compact context
// display. Exactly one key is non-empty when usage is present; all empty when
// there are no context tokens. Callers write every key so stale tiers clear.
func ContextTierTokenValues(usage ContextUsage) map[string]string {
	out := map[string]string{
		ContextTokenOK:       "",
		ContextTokenSoft:     "",
		ContextTokenWarn:     "",
		ContextTokenCritical: "",
	}
	name := ContextTierTokenName(usage.ContextTokens)
	if name == "" {
		return out
	}
	out[name] = FormatCompactContextTokens(usage)
	return out
}

// UsageStatusCandidates returns candidates in priority order (longest -> shortest).
// The first element is the full representation.
func UsageStatusCandidates(usage ContextUsage) []string {
	tokenLabel := formatTokenCount(usage.ContextTokens)

	if usage.WindowTokens == nil {
		return []string{fmt.Sprintf("⛁ %s", tokenLabel), tokenLabel}
	}

	window := *usage.WindowTokens
	percent := int(math.Min(100, math.Round(float64(usage.ContextTokens)/float64(window)*100)))
	percentLabel := fmt.Sprintf("%d%%", percent)
	withTokens := fmt.Sprintf("%s (%s)", percentLabel, tokenLabel)
	return []string{
		fmt.Sprintf("%s %s", iconFor(&percent), withTokens),
		withTokens,
		percentLabel,
	}
}

// FormatUsageStatus picks the longest candidate that fits in MaxColumns.
// Falls back to the shortest if nothing fits.
func FormatUsageStatus(usage ContextUsage, options FormatUsageOptions) string {
	candidates := UsageStatusCandidates(usage)
	if len(candidates) == 0 {
		return ""
	}
	if options.MaxColumns == nil {
		return candidates[0]
	}
	maxColumns := *options.MaxColumns
	for _, candidate := range candidates {
		if DisplayWidth(candidate) <= maxColumns {
			return candidate
		}
	}
	return candidates[len(candidates)-1]
}
