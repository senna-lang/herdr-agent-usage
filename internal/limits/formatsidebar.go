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
	out := "Σ " + formatBurnTokens(tokens)
	if costUSD > 0 {
		out += " " + formatBurnCost(costUSD)
	}
	return out
}

// formatBurnCost compacts a USD amount, keeping small DeepSeek-style
// fractions-of-a-cent readable while collapsing larger spend to cents/whole
// dollars: $0.0078, $0.42, $128.
func formatBurnCost(usd float64) string {
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

// formatBurnTokens compacts a token count (812, 9.5k, 350k, 5.7M).
// Same tiering as the context meter's count, plus an M tier: 5h burn
// across cache reads routinely exceeds 1M tokens.
func formatBurnTokens(tokens float64) string {
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
