package limits

import "math"

// WindowRemaining returns one window's remaining percentage, clamped to 0-100.
func WindowRemaining(window *LimitWindow) (int, bool) {
	if window == nil {
		return 0, false
	}
	value := int(math.Round(100 - window.UsedPercentage))
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return value, true
}

// MostConstrainedRemaining returns the lowest remaining percentage among a
// provider's known windows. This is the safest compact account-level summary.
func MostConstrainedRemaining(provider ProviderLimits) (int, bool) {
	windows := []*LimitWindow{provider.Primary, provider.Secondary, provider.Tertiary}
	remaining := 101
	found := false
	for _, window := range windows {
		value, ok := WindowRemaining(window)
		if !ok {
			continue
		}
		if !found || value < remaining {
			remaining = value
			found = true
		}
	}
	return remaining, found
}
