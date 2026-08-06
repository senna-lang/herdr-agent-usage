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
	"strings"
)

const warningThresholdPercent = 80

// FormatUsageOptions controls sidebar formatting.
type FormatUsageOptions struct {
	// MaxColumns is the maximum display width usable by the context token.
	// When nil, returns the full representation.
	MaxColumns *int
	// Fraction renders "130k/200k" instead of "65% (130k)" when the window
	// size is known ([display] context_display = "fraction" in plugin config).
	Fraction bool
	// IconStyle selects the leading glyph: "database" (⛁, ⚠️ from 80%),
	// "gauge" (▁▂▄▆█ by fill level), or "none".
	IconStyle string
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

// gaugeGlyphs are Block Elements: a single font renders the whole set, so the
// glyphs stay the same size (unlike the mixed-block circle set ○◔◑◕●).
var gaugeGlyphs = []string{"▁", "▂", "▄", "▆", "█"}

// GaugeLevelHotPercent is where the gauge shows a full block; it matches the
// "hot" context-level threshold so glyph and color escalate together.
const GaugeLevelHotPercent = 85

// GaugeLevelWarmPercent is where the gauge reaches its second-highest glyph
// and the context level turns "warm".
const GaugeLevelWarmPercent = 60

func gaugeFor(percent int) string {
	switch {
	case percent >= GaugeLevelHotPercent:
		return gaugeGlyphs[4]
	case percent >= GaugeLevelWarmPercent:
		return gaugeGlyphs[3]
	case percent >= 40:
		return gaugeGlyphs[2]
	case percent >= 20:
		return gaugeGlyphs[1]
	default:
		return gaugeGlyphs[0]
	}
}

// ContextLevelFor classifies window fill for level-token routing: "" (normal),
// "warm", or "hot". Thresholds are shared with the gauge glyphs so color and
// glyph escalate together.
func ContextLevelFor(percent *int) string {
	if percent == nil {
		return ""
	}
	switch {
	case *percent >= GaugeLevelHotPercent:
		return "hot"
	case *percent >= GaugeLevelWarmPercent:
		return "warm"
	}
	return ""
}

// iconForStyle returns the leading glyph for the chosen style, or "" when the
// style shows no icon (style "none", or a gauge with an unknown window).
func iconForStyle(style string, percent *int) string {
	switch style {
	case "gauge":
		if percent == nil {
			return ""
		}
		return gaugeFor(*percent)
	case "none":
		return ""
	default:
		return iconFor(percent)
	}
}

func formatTokenCount(tokens int) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	if tokens >= 1_000_000 {
		millions := float64(tokens) / 1_000_000
		if millions < 10 {
			return fmt.Sprintf("%.1fm", millions)
		}
		return fmt.Sprintf("%.0fm", millions)
	}
	thousands := float64(tokens) / 1000
	if thousands < 10 {
		return fmt.Sprintf("%.1fk", thousands)
	}
	return fmt.Sprintf("%.0fk", thousands)
}

// UsagePercent returns the window fill percentage (capped at 100), or nil
// when the window size is unknown.
func UsagePercent(usage ContextUsage) *int {
	if usage.WindowTokens == nil || *usage.WindowTokens <= 0 {
		return nil
	}
	percent := int(math.Min(100, math.Round(float64(usage.ContextTokens)/float64(*usage.WindowTokens)*100)))
	return &percent
}

// UsageStatusCandidates returns candidates in priority order (longest -> shortest).
// The first element is the full representation.
func UsageStatusCandidates(usage ContextUsage) []string {
	return usageStatusCandidates(usage, false, "")
}

// withIcon prefixes label with the style's glyph; a style without a glyph
// contributes no extra candidate.
func withIcon(iconStyle string, percent *int, label string, shorter ...string) []string {
	candidates := []string{label}
	if icon := iconForStyle(iconStyle, percent); icon != "" {
		candidates = []string{fmt.Sprintf("%s %s", icon, label), label}
	}
	return append(candidates, shorter...)
}

func usageStatusCandidates(usage ContextUsage, fraction bool, iconStyle string) []string {
	tokenLabel := formatTokenCount(usage.ContextTokens)
	percent := UsagePercent(usage)

	if percent == nil {
		return withIcon(iconStyle, nil, tokenLabel)
	}

	if fraction {
		fractionLabel := fmt.Sprintf("%s/%s", tokenLabel, formatTokenCount(*usage.WindowTokens))
		return withIcon(iconStyle, percent, fractionLabel, tokenLabel)
	}
	percentLabel := fmt.Sprintf("%d%%", *percent)
	withTokens := fmt.Sprintf("%s (%s)", percentLabel, tokenLabel)
	return withIcon(iconStyle, percent, withTokens, percentLabel)
}

// sidebarTokenSeparatorColumns is the " · " Herdr renders between sidebar row
// tokens (herdr 0.7.5).
const sidebarTokenSeparatorColumns = 3

// alignPadRune fills the right-align gap. Braille Pattern Blank renders as an
// empty cell but is not Unicode White_Space, so Herdr's token-value trim
// leaves it alone (regular spaces get stripped).
const alignPadRune = '⠀'

// PadStatusRight left-pads status so that leftLabel + separator + status spans
// exactly rowColumns, putting the meter's right edge flush with the sidebar.
// Returns status unchanged when it already fills or overflows the row.
func PadStatusRight(status string, leftLabelColumns, rowColumns int) string {
	pad := rowColumns - leftLabelColumns - sidebarTokenSeparatorColumns - DisplayWidth(status)
	if pad <= 0 {
		return status
	}
	return strings.Repeat(string(alignPadRune), pad) + status
}

// FormatUsageStatus picks the longest candidate that fits in MaxColumns.
// Falls back to the shortest if nothing fits.
func FormatUsageStatus(usage ContextUsage, options FormatUsageOptions) string {
	candidates := usageStatusCandidates(usage, options.Fraction, options.IconStyle)
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
