/**
 * Builds the rate-limit summary string shown in the statusLine.
 */
package ratelimit

import (
	"fmt"
	"math"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// FormatStatusLineSummary formats % for present windows (e.g. "5h:11% 7d:56%").
// percent selects remaining (default) vs used; the number is inverted in used mode.
func FormatStatusLineSummary(rateLimits *RateLimitsInput, percent core.LimitPercent) string {
	if rateLimits == nil {
		return ""
	}
	var parts []string
	if rateLimits.FiveHour != nil {
		parts = append(parts, fmt.Sprintf("5h:%s", percentageLabel(rateLimits.FiveHour.UsedPercentage, percent)))
	}
	if rateLimits.SevenDay != nil {
		parts = append(parts, fmt.Sprintf("7d:%s", percentageLabel(rateLimits.SevenDay.UsedPercentage, percent)))
	}
	return strings.Join(parts, " ")
}

func percentageLabel(usedPercentage float64, percent core.LimitPercent) string {
	remaining := int(math.Round(100 - usedPercentage))
	return fmt.Sprintf("%d%%", percent.DisplayPercent(remaining))
}
