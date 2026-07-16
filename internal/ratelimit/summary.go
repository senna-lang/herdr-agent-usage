/**
 * Builds the rate-limit summary string shown in the statusLine.
 */
package ratelimit

import (
	"fmt"
	"math"
	"strings"
)

// FormatStatusLineSummary formats remaining % for present windows (e.g. "5h:11% 7d:56%").
func FormatStatusLineSummary(rateLimits *RateLimitsInput) string {
	if rateLimits == nil {
		return ""
	}
	var parts []string
	if rateLimits.FiveHour != nil {
		parts = append(parts, fmt.Sprintf("5h:%s", remainingPercentageLabel(rateLimits.FiveHour.UsedPercentage)))
	}
	if rateLimits.SevenDay != nil {
		parts = append(parts, fmt.Sprintf("7d:%s", remainingPercentageLabel(rateLimits.SevenDay.UsedPercentage)))
	}
	return strings.Join(parts, " ")
}

func remainingPercentageLabel(usedPercentage float64) string {
	remaining := 100 - usedPercentage
	return fmt.Sprintf("%d%%", int(math.Round(remaining)))
}
