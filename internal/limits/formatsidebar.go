/**
 * Formats a provider limit for Herdr's compact sidebar metadata row.
 */
package limits

import "fmt"

// FormatSidebarLimit returns the shortest available provider window as a
// standalone sidebar row. Context usage remains in its own $context row.
func FormatSidebarLimit(provider ProviderLimits) string {
	var window *LimitWindow
	fallback := ""
	switch {
	case provider.Primary != nil:
		window, fallback = provider.Primary, "5h"
	case provider.Secondary != nil:
		window, fallback = provider.Secondary, "7d"
	case provider.Tertiary != nil:
		window, fallback = provider.Tertiary, "30d"
	default:
		return ""
	}
	return fmt.Sprintf("%s %d%%", windowTag(window, fallback), remainingOf(window.UsedPercentage))
}
