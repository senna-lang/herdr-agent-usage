/**
 * Pure function that computes the per-pane token activity share among the
 * open panes for a given provider. The share is each pane's windowed token
 * total divided by the per-provider sum.
 */
package limits

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// PaneTokenRow is one open pane's windowed token total.
type PaneTokenRow struct {
	PaneID string
	Label  string
	Tokens float64
}

// PaneActivityShare is a pane's share of provider activity.
type PaneActivityShare struct {
	PaneID string
	Label  string
	Tokens float64
	// SharePercent is 0-100. Zero when the sum is zero.
	SharePercent float64
}

// shortPaneSuffix returns the trailing segment of a pane id (e.g. "w6:p1" -> "p1").
func shortPaneSuffix(paneID string) string {
	idx := strings.LastIndex(paneID, ":")
	if idx >= 0 {
		return paneID[idx+1:]
	}
	return paneID
}

// DisambiguateLabels appends a short pane-id suffix to any label that repeats
// within the list, so e.g. two un-renamed Claude panes render as
// "claude p1" / "claude pC" instead of being indistinguishable.
// Labels that are already unique are left untouched.
func DisambiguateLabels(items []PaneTokenRow) []PaneTokenRow {
	counts := make(map[string]int, len(items))
	for _, it := range items {
		counts[it.Label]++
	}
	out := make([]PaneTokenRow, len(items))
	for i, it := range items {
		out[i] = it
		if counts[it.Label] > 1 {
			out[i].Label = it.Label + " " + shortPaneSuffix(it.PaneID)
		}
	}
	return out
}

// DefaultSmallestWindowMinutes is 5 hours.
// When Codex's primary windowMinutes (or any other provider's) is available,
// that takes precedence.
const DefaultSmallestWindowMinutes = 300

// ResolveActivityWindowMinutes returns primary window minutes or the 5h default.
func ResolveActivityWindowMinutes(primaryWindowMinutes *int) int {
	if primaryWindowMinutes != nil && *primaryWindowMinutes > 0 {
		return *primaryWindowMinutes
	}
	return DefaultSmallestWindowMinutes
}

// WindowStartMs subtracts windowMinutes from nowMs.
func WindowStartMs(nowMs int64, windowMinutes int) int64 {
	return nowMs - int64(windowMinutes)*60_000
}

// ComputePaneActivityShares computes activity share from token rows.
// Rows with tokens <= 0 are dropped. Display order: tokens desc, then paneId asc.
func ComputePaneActivityShares(rows []PaneTokenRow) (totalTokens float64, panes []PaneActivityShare) {
	positive := make([]PaneTokenRow, 0, len(rows))
	for _, r := range rows {
		if math.IsNaN(r.Tokens) || math.IsInf(r.Tokens, 0) || r.Tokens <= 0 {
			continue
		}
		positive = append(positive, r)
	}
	for _, r := range positive {
		totalTokens += r.Tokens
	}
	panes = make([]PaneActivityShare, 0, len(positive))
	for _, r := range positive {
		share := 0.0
		if totalTokens > 0 {
			share = math.Round((r.Tokens/totalTokens)*1000) / 10
		}
		panes = append(panes, PaneActivityShare{
			PaneID:       r.PaneID,
			Label:        r.Label,
			Tokens:       r.Tokens,
			SharePercent: share,
		})
	}
	sort.Slice(panes, func(i, j int) bool {
		if panes[i].Tokens != panes[j].Tokens {
			return panes[i].Tokens > panes[j].Tokens
		}
		return panes[i].PaneID < panes[j].PaneID
	})
	return totalTokens, panes
}

// OtherPaneID is a synthetic entry for closed panes / other sessions.
const OtherPaneID = "__other__"

// OtherLabel is the display label for OtherPaneID.
const OtherLabel = "closed / other"

// ComputeSharesWithOther is like ComputePaneActivityShares, but scales open-pane
// shares against the provider's total window tokens and appends a trailing
// "closed / other" bucket for the remainder. The bucket is omitted when it
// rounds below 0.1%.
func ComputeSharesWithOther(rows []PaneTokenRow, providerTotalTokens float64) (totalTokens float64, panes []PaneActivityShare) {
	openTokens, openPanes := ComputePaneActivityShares(rows)
	grand := providerTotalTokens
	if math.IsNaN(grand) || math.IsInf(grand, 0) {
		grand = 0
	}
	if openTokens > grand {
		grand = openTokens
	}
	if grand <= 0 {
		return 0, nil
	}

	rescaled := make([]PaneActivityShare, len(openPanes))
	for i, x := range openPanes {
		rescaled[i] = x
		rescaled[i].SharePercent = math.Round((x.Tokens/grand)*1000) / 10
	}
	otherTokens := math.Max(0, grand-openTokens)
	otherShare := math.Round((otherTokens/grand)*1000) / 10
	if otherShare < 0.1 {
		return grand, rescaled
	}
	return grand, append(rescaled, PaneActivityShare{
		PaneID:       OtherPaneID,
		Label:        OtherLabel,
		Tokens:       otherTokens,
		SharePercent: otherShare,
	})
}

// FormatTokenCount renders a short human-friendly token display (1.2M / 340k / 512).
func FormatTokenCount(tokens float64) string {
	if math.IsNaN(tokens) || math.IsInf(tokens, 0) || tokens < 0 {
		return "0"
	}
	if tokens >= 1_000_000 {
		return trimFloat(tokens/1_000_000) + "M"
	}
	if tokens >= 1_000 {
		return trimFloat(tokens/1_000) + "k"
	}
	return strconv.FormatInt(int64(math.Round(tokens)), 10)
}

func trimFloat(n float64) string {
	rounded := math.Round(n*10) / 10
	if rounded == math.Trunc(rounded) {
		return strconv.FormatInt(int64(rounded), 10)
	}
	return strconv.FormatFloat(rounded, 'f', 1, 64)
}
