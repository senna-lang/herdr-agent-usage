/**
 * Formats a provider limit for Herdr's compact sidebar metadata row.
 */
package limits

import "fmt"

// FormatSidebarLimit returns the shortest unexpired provider window as a
// standalone sidebar row. Context usage remains in its own $context row.
func FormatSidebarLimit(provider ProviderLimits, nowMs int64) string {
	candidates := []struct {
		window   *LimitWindow
		fallback string
	}{
		{provider.Primary, "5h"},
		{provider.Secondary, "7d"},
		{provider.Tertiary, "30d"},
	}
	for _, candidate := range candidates {
		window := candidate.window
		if window == nil {
			continue
		}
		if window.ResetsAt != nil && *window.ResetsAt > 0 && *window.ResetsAt <= nowMs/1000 {
			continue
		}
		return fmt.Sprintf("%s %d%%", windowTag(window, candidate.fallback), remainingOf(window.UsedPercentage))
	}
	return ""
}
