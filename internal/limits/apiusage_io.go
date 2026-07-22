/**
 * I/O for pay-as-you-go backend blocks: discovers which backends the open
 * OpenCode panes are actually running, then aggregates their spend.
 *
 * One 30d fetch per backend feeds every window, the model breakdown, and the
 * pane shares — the panel refreshes on a 15s ticker against a large DB, so
 * re-scanning per window would be wasteful.
 */
package limits

import (
	"database/sql"
	"os"
	"sort"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
	_ "modernc.org/sqlite"
)

// openCodeGoBackendID is the subscription gateway; it has real quota windows
// and is rendered as a subscription block, never as a spend block.
const openCodeGoBackendID = "opencode-go"

// paneBackend pairs an open pane with the backend its session last used.
type paneBackend struct {
	Pane      OpenPaneSnapshot
	BackendID string
}

// activeAPIPaneBackends returns the pay-as-you-go backends in use by open
// panes of one harness. Subscription panes and panes whose backend cannot be
// resolved are skipped.
func activeAPIPaneBackends(openPanes []OpenPaneSnapshot, harnessID string) []paneBackend {
	// Resolve the billing deps (and their profile snapshot) once for the whole
	// pass rather than per pane.
	deps := DefaultBillingDeps()
	var out []paneBackend
	for _, pane := range openPanes {
		if agentToProvider[strings.ToLower(pane.Agent)] != harnessID {
			continue
		}
		if PaneBillingMode(harnessID, pane, deps) != BillingPayAsYouGo {
			continue
		}
		backendID := payAsYouGoBackendID(harnessID, pane)
		if backendID == "" {
			continue
		}
		out = append(out, paneBackend{Pane: pane, BackendID: backendID})
	}
	return out
}

// CollectAPIProviderUsage builds one block per pay-as-you-go backend running
// in an open pane across every harness, richest first.
//
// Only OpenCode records a per-message cost, so blocks from the other
// harnesses are token-only; the formatter drops the cost column for them.
func CollectAPIProviderUsage(openPanes []OpenPaneSnapshot, nowMs int64) []APIProviderUsage {
	out := collectOpenCodeAPIUsage(openPanes, nowMs)
	out = append(out, collectFileHarnessAPIUsage(openPanes, nowMs)...)
	sortAPIProviderUsage(out)
	return out
}

// collectOpenCodeAPIUsage builds blocks from the OpenCode SQLite store, the
// only source that carries cost.
func collectOpenCodeAPIUsage(openPanes []OpenPaneSnapshot, nowMs int64) []APIProviderUsage {
	active := activeAPIPaneBackends(openPanes, "opencode")
	if len(active) == 0 {
		return nil
	}

	dbPath := ResolveOpenCodeLimitsDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()

	longestWindow := APIUsageWindowMinutes[len(APIUsageWindowMinutes)-1]
	startMs := WindowStartMs(nowMs, longestWindow)

	// Preserve first-seen backend order, then sort the finished blocks.
	seen := make(map[string]bool)
	var backendIDs []string
	panesByBackend := make(map[string][]OpenPaneSnapshot)
	for _, ab := range active {
		if !seen[ab.BackendID] {
			seen[ab.BackendID] = true
			backendIDs = append(backendIDs, ab.BackendID)
		}
		panesByBackend[ab.BackendID] = append(panesByBackend[ab.BackendID], ab.Pane)
	}

	var out []APIProviderUsage
	for _, backendID := range backendIDs {
		block := buildAPIProviderUsage(db, backendID, panesByBackend[backendID], startMs, nowMs)
		if block == nil {
			continue
		}
		out = append(out, *block)
	}
	return out
}

func buildAPIProviderUsage(db *sql.DB, backendID string, panes []OpenPaneSnapshot, startMs, nowMs int64) *APIProviderUsage {
	rows := decodeBackendRows(db, backendID, startMs, nowMs)
	if len(rows) == 0 {
		return nil
	}
	windows := SumAPIWindows(rows, nowMs, APIUsageWindowMinutes)

	label := opencode.ProviderDisplayName(backendID)
	if label == "" {
		label = HumanizeBackendID(backendID)
	}

	block := APIProviderUsage{
		BackendID: backendID,
		Label:     label,
		Windows:   windows,
		Models:    SumAPIModels(rows, nowMs, APIShareWindowMinutes),
		HasCost:   AnyAPICost(windows),
	}
	if activity := apiPaneActivity(db, backendID, panes, nowMs); activity != nil {
		block.PaneActivity = activity
	}
	return &block
}

// decodeBackendRows loads one backend's assistant messages over the longest
// window. Filtering by providerID in SQL keeps the decode set small.
func decodeBackendRows(db *sql.DB, backendID string, startMs, nowMs int64) []apiUsageRow {
	rows, err := db.Query(
		`SELECT data, CAST(COALESCE(json_extract(data, '$.time.created'), time_created) AS INTEGER) AS created
		 FROM message
		 WHERE json_valid(data)
		   AND json_extract(data, '$.role') = 'assistant'
		   AND json_extract(data, '$.providerID') = ?
		   AND CAST(COALESCE(json_extract(data, '$.time.created'), time_created) AS INTEGER) BETWEEN ? AND ?`,
		backendID, startMs, nowMs,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var list []OpenCodeTokenRow
	for rows.Next() {
		var data string
		var created int64
		if err := rows.Scan(&data, &created); err != nil {
			continue
		}
		list = append(list, OpenCodeTokenRow{Data: data, TimeCreated: created})
	}
	return DecodeAPIUsageRows(list, backendID)
}

// apiPaneActivity computes the 24h per-pane token share for one backend,
// scaled against the backend's total so closed sessions land in "other" —
// matching how subscription blocks compute their share rows.
func apiPaneActivity(db *sql.DB, backendID string, panes []OpenPaneSnapshot, nowMs int64) *ProviderPaneActivity {
	startMs := WindowStartMs(nowMs, APIShareWindowMinutes)

	rawRows := make([]PaneTokenRow, 0, len(panes))
	for _, pane := range panes {
		sessionID := resolvePaneSessionID(db, pane)
		if sessionID == "" {
			continue
		}
		tokens := backendTokensForSession(db, sessionID, backendID, startMs, nowMs)
		rawRows = append(rawRows, PaneTokenRow{PaneID: pane.PaneID, Label: pane.Label, Tokens: tokens})
	}
	if len(rawRows) == 0 {
		return nil
	}

	backendTotal := backendTokensTotal(db, backendID, startMs, nowMs)
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

func resolvePaneSessionID(db *sql.DB, pane OpenPaneSnapshot) string {
	if sid := sessionIDStr(pane); sid != "" {
		return sid
	}
	cwd := cwdStr(pane)
	if cwd == "" {
		return ""
	}
	var sessionID string
	_ = db.QueryRow(
		`SELECT id FROM session WHERE directory = ? AND time_archived IS NULL ORDER BY time_updated DESC LIMIT 1`,
		cwd,
	).Scan(&sessionID)
	return sessionID
}

func backendTokensForSession(db *sql.DB, sessionID, backendID string, startMs, nowMs int64) float64 {
	rows, err := db.Query(
		`SELECT data, CAST(COALESCE(json_extract(data, '$.time.created'), time_created) AS INTEGER)
		 FROM message
		 WHERE session_id = ?
		   AND json_valid(data)
		   AND json_extract(data, '$.role') = 'assistant'
		   AND json_extract(data, '$.providerID') = ?
		   AND CAST(COALESCE(json_extract(data, '$.time.created'), time_created) AS INTEGER) BETWEEN ? AND ?`,
		sessionID, backendID, startMs, nowMs,
	)
	if err != nil {
		return 0
	}
	defer rows.Close()
	var sum float64
	for rows.Next() {
		var data string
		var created int64
		if err := rows.Scan(&data, &created); err != nil {
			continue
		}
		for _, r := range DecodeAPIUsageRows([]OpenCodeTokenRow{{Data: data, TimeCreated: created}}, backendID) {
			sum += r.Tokens
		}
	}
	return sum
}

func backendTokensTotal(db *sql.DB, backendID string, startMs, nowMs int64) float64 {
	var sum float64
	for _, r := range decodeBackendRows(db, backendID, startMs, nowMs) {
		sum += r.Tokens
	}
	return sum
}

func sortAPIProviderUsage(blocks []APIProviderUsage) {
	sort.SliceStable(blocks, func(i, j int) bool {
		a, b := blocks[i], blocks[j]
		aw, bw := firstWindow(a), firstWindow(b)
		if aw.CostUSD != bw.CostUSD {
			return aw.CostUSD > bw.CostUSD
		}
		if aw.Tokens != bw.Tokens {
			return aw.Tokens > bw.Tokens
		}
		return a.BackendID < b.BackendID
	})
}

func firstWindow(p APIProviderUsage) APIUsageWindow {
	if len(p.Windows) == 0 {
		return APIUsageWindow{}
	}
	return p.Windows[0]
}

// opencodeCatalogLabel exposes OpenCode's provider catalog name to the other
// harnesses' blocks: the catalog covers most vendor ids regardless of which
// harness reached them.
func opencodeCatalogLabel(backendID string) string {
	return opencode.ProviderDisplayName(backendID)
}
