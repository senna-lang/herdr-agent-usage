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
	"strings"
	"sync/atomic"

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
func FormatSidebarLimit(provider ProviderLimits, nowMs int64, percent core.LimitPercent) string {

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
	switch sidebarWindowsMode.Load() {
	case "all":
		if text := formatAllWindows(candidates, percent); text != "" {
			return text
		}
	case "compact":
		if text := formatCompactWindows(candidates, percent, nowMs); text != "" {
			return text
		}
	}
	return fmt.Sprintf("%s %d%%", windowTag(shortest.window, shortest.fallback), percent.DisplayPercent(remainingOf(shortest.window.UsedPercentage)))

}

// sidebarWindowsMode selects the $limit presentation: "" (shortest window),
// "all" (every window, tagged) or "compact" (every window, percentages only).
// main sets it from [ui] sidebar_windows so the default stays unchanged.
var sidebarWindowsMode atomic.Value

// SetSidebarWindows selects the $limit presentation mode. Unknown values fall
// back to the default shortest-window row.
func SetSidebarWindows(mode string) {
	switch mode {
	case "all", "compact":
		sidebarWindowsMode.Store(mode)
	default:
		sidebarWindowsMode.Store("")
	}
}

// SidebarWindowBand is one window's contribution to a per-number coloured
// row: its slot tag, the text (including the trailing separator unless it is
// the last present window), and the colour band that text belongs to.
type SidebarWindowBand struct {
	Tag  string
	Text string
	Band core.LimitBand
}

// SidebarWindowTags is the fixed slot order a per-number row renders in.
var SidebarWindowTags = []string{"5h", "7d", "30d"}

func sidebarWindowPick(tag string, p ProviderLimits) *LimitWindow {
	switch tag {
	case "5h":
		return p.Primary
	case "7d":
		return p.Secondary
	default:
		return p.Tertiary
	}
}

// FormatSidebarWindowBands splits every present window into its own banded
// token so a layout can colour each number separately. Text carries no
// separator: Herdr already joins row items with "·", so adding one would
// double it.
func FormatSidebarWindowBands(p ProviderLimits, percent core.LimitPercent, nowMs int64) []SidebarWindowBand {
	bands := make([]SidebarWindowBand, 0, len(SidebarWindowTags))
	for _, tag := range SidebarWindowTags {
		window := sidebarWindowPick(tag, p)
		if window == nil {
			continue
		}
		remaining := 100 - window.UsedPercentage
		if remaining < 0 {
			remaining = 0
		}
		bands = append(bands, SidebarWindowBand{
			Tag:  tag,
			Text: sidebarWindowText(window, percent, nowMs),
			Band: core.LimitBandFor(remaining),
		})
	}
	return bands
}

// MinRemainingPercent returns the tightest headroom across the provider's
// present windows, or -1 when it has none. It answers "how close is this pane
// to a wall" rather than "how is the current window doing", so the sidebar
// colour warns about the 30d window even while 5h is comfortable.
func MinRemainingPercent(provider ProviderLimits) float64 {
	minRemaining := -1.0
	for _, window := range []*LimitWindow{provider.Primary, provider.Secondary, provider.Tertiary} {
		if window == nil {
			continue
		}
		remaining := 100 - window.UsedPercentage
		if remaining < 0 {
			remaining = 0
		}
		if minRemaining < 0 || remaining < minRemaining {
			minRemaining = remaining
		}
	}
	return minRemaining
}

// formatAllWindows renders every present window in slot order.
func formatAllWindows(candidates []struct {
	window   *LimitWindow
	fallback string
}, percent core.LimitPercent) string {
	parts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.window == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d%%",
			windowTag(candidate.window, candidate.fallback),
			percent.DisplayPercent(remainingOf(candidate.window.UsedPercentage))))
	}
	return strings.Join(parts, " · ")
}

// formatCompactWindows renders every present window as bare percentages, e.g.
// "96% · 97% · 37%". Window order is the slot order, so the numbers mean
// 5h/7d/30d.
func formatCompactWindows(candidates []struct {
	window   *LimitWindow
	fallback string
}, percent core.LimitPercent, nowMs int64) string {
	parts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.window == nil {
			continue
		}
		parts = append(parts, sidebarWindowText(candidate.window, percent, nowMs))
	}
	return strings.Join(parts, " · ")
}

// sidebarWindowText renders one window as "<percent> <countdown>", dropping the
// countdown when the reset time is unknown.
func sidebarWindowText(window *LimitWindow, percent core.LimitPercent, nowMs int64) string {
	text := fmt.Sprintf("%d%%", percent.DisplayPercent(remainingOf(window.UsedPercentage)))
	if window.ResetsAt != nil {
		if countdown := core.FormatSidebarCountdown(*window.ResetsAt - nowMs/1000); countdown != "" {
			text += " " + countdown
		}
	}
	return text
}
