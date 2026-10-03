/**
 * Pay-as-you-go block collection for Kilo, whose usage lives in its own
 * SQLite session store alongside the context rows the kilo provider reads.
 *
 * Kilo drives several backends from one CLI, and a session can move between
 * them, so every figure here is keyed by the backend that served the turn. That
 * is what makes the block's label honest: the pane is labelled with the backend
 * it is on now, and only that backend's spend is counted under it.
 *
 * The scan is gated twice, like the file-backed harnesses: Kilo is read at all
 * only when it has an open pay-as-you-go pane, and the store itself narrows the
 * read to the sessions that can hold a message in the window (see
 * kilo.SpendByBackend), because the panel refreshes on a 15s ticker against a
 * multi-gigabyte live database.
 */
package limits

import (
	"github.com/senna-lang/herdr-agent-usage/internal/providers/kilo"
)

// collectKiloAPIUsage builds one block per pay-as-you-go backend a Kilo pane is
// running.
func collectKiloAPIUsage(openPanes []OpenPaneSnapshot, nowMs int64) []APIProviderUsage {
	active := activeAPIPaneBackends(openPanes, kilo.Provider.AgentID())
	if len(active) == 0 {
		return nil
	}

	// Rows for every session on disk in the window, keyed by backend, plus the
	// subset that belongs to each open pane (for the share row).
	rowsByBackend := kilo.SpendByBackend(
		WindowStartMs(nowMs, APIUsageWindowMinutes[len(APIUsageWindowMinutes)-1]), nowMs)

	backendOrder := make([]string, 0, len(active))
	seen := make(map[string]bool)
	panesByBackend := make(map[string][]OpenPaneSnapshot)
	for _, ab := range active {
		if !seen[ab.BackendID] {
			seen[ab.BackendID] = true
			backendOrder = append(backendOrder, ab.BackendID)
		}
		panesByBackend[ab.BackendID] = append(panesByBackend[ab.BackendID], ab.Pane)
	}

	var out []APIProviderUsage
	for _, backendID := range backendOrder {
		rows := DecodeAPIUsageRows(kiloSpendRows(rowsByBackend[backendID]), backendID)
		if len(rows) == 0 {
			continue
		}
		windows := SumAPIWindows(rows, nowMs, APIUsageWindowMinutes)
		block := APIProviderUsage{
			BackendID: backendID,
			Label:     backendDisplayLabel(backendID),
			Windows:   windows,
			Models:    SumAPIModels(rows, nowMs, APIShareWindowMinutes),
			HasCost:   AnyAPICost(windows),
		}
		if activity := kiloPaneActivity(backendID, panesByBackend[backendID], rows, nowMs); activity != nil {
			block.PaneActivity = activity
		}
		out = append(out, block)
	}
	return out
}

// kiloPaneActivity scales each open pane's own spend on this backend against the
// backend's total, matching how the other blocks compute their share row. A pane
// that has since moved to another backend contributes nothing here: it belongs to
// that backend's block, not this one.
func kiloPaneActivity(backendID string, panes []OpenPaneSnapshot, backendRows []apiUsageRow, nowMs int64) *ProviderPaneActivity {
	startMs := WindowStartMs(nowMs, APIShareWindowMinutes)

	rawRows := make([]PaneTokenRow, 0, len(panes))
	for _, pane := range panes {
		if payAsYouGoBackendID(kilo.Provider.AgentID(), pane) != backendID {
			continue
		}
		var tokens float64
		for _, usage := range DecodeAPIUsageRows(
			kiloSpendRows(kilo.PaneSpend(pane.SessionID, pane.Cwd, backendID, startMs, nowMs)), backendID) {
			tokens += usage.Tokens
		}
		rawRows = append(rawRows, PaneTokenRow{PaneID: pane.PaneID, Label: pane.Label, Tokens: tokens})
	}
	if len(rawRows) == 0 {
		return nil
	}

	var backendTotal float64
	for _, row := range backendRows {
		if row.CreatedMs >= startMs && row.CreatedMs <= nowMs {
			backendTotal += row.Tokens
		}
	}
	totalTokens, shares := ComputeSharesWithOther(DisambiguateLabels(rawRows), backendTotal)
	if len(shares) == 0 {
		return nil
	}
	return &ProviderPaneActivity{
		WindowMinutes: APIShareWindowMinutes,
		TotalTokens:   int(totalTokens),
		Panes:         shares,
	}
}
