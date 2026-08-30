/**
 * LimitPercent is the presentation direction for quota percentages.
 *
 * remaining (default) shows headroom; used shows consumption. Notify
 * thresholds and UsedPercentage stay remaining-based; only rendered
 * numbers, words, and bar fill follow this flag. Colour still tracks
 * remaining headroom in both modes.
 */
package core

import (
	"strconv"
	"strings"
)

// LimitPercent is a two-value presentation enum for quota percentages.
type LimitPercent string

const (
	// LimitPercentRemaining shows headroom (% left / % remaining). Default.
	LimitPercentRemaining LimitPercent = "remaining"
	// LimitPercentUsed shows consumption (% used). Bar fill follows the number.
	LimitPercentUsed LimitPercent = "used"
)

// ParseLimitPercent resolves a config value. Unknown or empty input is remaining.
func ParseLimitPercent(raw string) LimitPercent {
	if strings.EqualFold(strings.TrimSpace(raw), string(LimitPercentUsed)) {
		return LimitPercentUsed
	}
	return LimitPercentRemaining
}

// Used reports whether percentages should render as consumption.
func (m LimitPercent) Used() bool {
	return m == LimitPercentUsed
}

// DisplayPercent converts a remaining 0–100 value into the number shown to the user.
func (m LimitPercent) DisplayPercent(remaining int) int {
	if m.Used() {
		return 100 - remaining
	}
	return remaining
}

// BarFill is the 0–100 fraction passed to the gauge. It matches DisplayPercent
// so the bar and the number always move in the same direction.
func (m LimitPercent) BarFill(remaining int) float64 {
	return float64(m.DisplayPercent(remaining))
}

// InvertRemainingBucket maps a remaining-percent threshold to the displayed
// number. used mode: 20 remaining → 80. The live window percentage is not used.
func (m LimitPercent) InvertRemainingBucket(remainingBucket string) int {
	n, err := strconv.Atoi(remainingBucket)
	if err != nil {
		return 0
	}
	return m.DisplayPercent(n)
}
