/**
 * Formats a provider limit for Herdr's compact sidebar metadata row.
 * Pay-as-you-go panes have no subscription quota to report against, so they
 * get the pane's whole-session token/cost total (Σ Nk) instead of a
 * remaining-% row.
 */
package limits

import (
	"fmt"
	"math"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// FormatSidebarBurn renders a pane's total token consumption (and USD cost,
// when the harness reports it) for panes without subscription limits, e.g.
// "Σ 350k $0.42". costUSD is 0 when the harness has no local cost data
// (Claude/Codex/Grok today) — the cost segment is simply omitted then, since
// PaneTotalUsage only returns a nonzero cost when it actually read one. Zero
// tokens returns "" so idle panes keep a clean sidebar.
func FormatSidebarBurn(tokens float64, costUSD float64) string {
	if tokens < 1 {
		return ""
	}
	out := "Σ " + formatCompactTokens(tokens)
	if costUSD > 0 {
		out += " " + formatCompactCost(costUSD)
	}
	return out
}

// formatCompactCost compacts a USD amount, keeping small fractions-of-a-cent
// readable while collapsing larger spend to cents/whole dollars: $0.0078,
// $0.42, $128. Shared by the sidebar burn row and the panel's API blocks.
func formatCompactCost(usd float64) string {
	switch {
	case usd < 0.01:
		return fmt.Sprintf("$%.4f", usd)
	case usd < 10:
		return fmt.Sprintf("$%.2f", usd)
	default:
		// %.0f uses round-half-to-even; math.Round is the conventional
		// half-away-from-zero rounding users expect for a dollar amount.
		return fmt.Sprintf("$%.0f", math.Round(usd))
	}
}

// formatCompactTokens compacts a token count (812, 9.5k, 350k, 5.7M).
// Same tiering as the context meter's count, plus an M tier: burn totals
// across cache reads routinely exceed 1M tokens. Rounds harder than
// FormatTokenCount ("350k" vs "350.4k") since both the sidebar row and the
// panel's spend columns are width-constrained. Shared by both.
func formatCompactTokens(tokens float64) string {
	switch {
	case tokens < 1000:
		return fmt.Sprintf("%.0f", tokens)
	case tokens < 10_000:
		return fmt.Sprintf("%.1fk", tokens/1000)
	case tokens < 1_000_000:
		return fmt.Sprintf("%.0fk", tokens/1000)
	case tokens < 10_000_000:
		return fmt.Sprintf("%.1fM", tokens/1_000_000)
	default:
		return fmt.Sprintf("%.0fM", tokens/1_000_000)
	}
}

// FormatSidebarLimit returns the shortest available provider window as a
// standalone sidebar row. A window is not displaced merely because its last
// recorded reset time has passed: providers may refresh that window shortly
// after the boundary, and switching to a longer window makes the sidebar
// unstable. Collection freshness is handled by the provider adapters.
// Context usage remains in its own $context row. percent selects remaining
// (default) vs used presentation; the tag has no left/used suffix.
func FormatSidebarLimit(provider ProviderLimits, _ int64, percent core.LimitPercent) string {

	candidates := []struct {
		window   *LimitWindow
		fallback string
	}{
		{provider.Primary, "5h"},
		{provider.Secondary, "7d"},
		{provider.Tertiary, "30d"},
	}
	var fallback *struct {
		window   *LimitWindow
		fallback string
	}
	var shortest *struct {
		window   *LimitWindow
		fallback string
	}
	for _, candidate := range candidates {
		window := candidate.window
		if window == nil {
			continue
		}
		candidate := candidate
		if fallback == nil {
			fallback = &candidate
		}
		if window.WindowMinutes == nil || *window.WindowMinutes <= 0 {
			continue
		}
		if shortest == nil || *window.WindowMinutes < *shortest.window.WindowMinutes {
			shortest = &candidate
		}
	}
	if shortest == nil {
		shortest = fallback
	}
	if shortest == nil {
		return ""
	}
	return fmt.Sprintf("%s %d%%", windowTag(shortest.window, shortest.fallback), percent.DisplayPercent(remainingOf(shortest.window.UsedPercentage)))

}
