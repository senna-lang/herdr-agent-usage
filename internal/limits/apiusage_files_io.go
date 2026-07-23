/**
 * Pay-as-you-go block collection for the file-backed harnesses
 * (Claude / Codex / Grok), whose usage lives in per-session transcripts
 * rather than a queryable store.
 *
 * Scanning every session over 30d is far heavier than the 5h activity scans,
 * and the panel refreshes on a 15s ticker, so the work is gated twice: a
 * harness is only scanned when it has an open pay-as-you-go pane, and within
 * that, only files touched inside the window are read (mtime filter).
 *
 * None of these harnesses record cost, so their blocks are token-only.
 */
package limits

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/omp"
)

// collectFileHarnessAPIUsage builds blocks for every file-backed harness that
// has an open pay-as-you-go pane.
func collectFileHarnessAPIUsage(openPanes []OpenPaneSnapshot, nowMs int64) []APIProviderUsage {
	var out []APIProviderUsage
	for _, harnessID := range []string{"claude", "codex", "grok", "omp", "pi"} {
		active := activeAPIPaneBackends(openPanes, harnessID)
		if len(active) == 0 {
			continue
		}
		out = append(out, buildFileHarnessBlocks(harnessID, active, nowMs)...)
	}
	return out
}

func buildFileHarnessBlocks(harnessID string, active []paneBackend, nowMs int64) []APIProviderUsage {
	startMs := WindowStartMs(nowMs, APIUsageWindowMinutes[len(APIUsageWindowMinutes)-1])

	// Rows for every session on disk, keyed by backend, plus the subset that
	// belongs to each open pane (for the share row).
	allRows := scanHarnessRows(harnessID, startMs, nowMs)
	if len(allRows) == 0 {
		return nil
	}

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
		rows := allRows[backendID]
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
		if activity := fileHarnessPaneActivity(harnessID, backendID, panesByBackend[backendID], rows, nowMs); activity != nil {
			block.PaneActivity = activity
		}
		out = append(out, block)
	}
	return out
}

// backendDisplayLabel prefers OpenCode's catalog name (it covers most vendor
// ids, including ones reached through other harnesses) and falls back to a
// humanized id.
func backendDisplayLabel(backendID string) string {
	if label := opencodeCatalogLabel(backendID); label != "" {
		return label
	}
	return HumanizeBackendID(backendID)
}

// scanHarnessRows returns usage rows grouped by backend for one harness.
func scanHarnessRows(harnessID string, startMs, nowMs int64) map[string][]apiUsageRow {
	switch harnessID {
	case "claude":
		// Transcripts lack per-session provider; use account deployment env.
		backendID := AccountClaudeBackendID()
		rows := scanClaudeRows(startMs)
		if len(rows) == 0 {
			return nil
		}
		return map[string][]apiUsageRow{backendID: rows}
	case "grok":
		return scanGrokRowsByBackend(startMs)
	case "codex":
		return scanCodexRows(startMs)
	case "omp":
		return scanOMPPiRowsByBackend(omp.ListAllOMPSessionFiles(), startMs)
	case "pi":
		return scanOMPPiRowsByBackend(omp.ListAllPiSessionFiles(), startMs)
	default:
		return nil
	}
}

func scanClaudeRows(startMs int64) []apiUsageRow {
	root := claudeProjectsRoot()
	var out []apiUsageRow
	for _, dir := range listDirSafe(root) {
		dirPath := filepath.Join(root, dir)
		for _, file := range listDirSafe(dirPath) {
			if !strings.HasSuffix(file, ".jsonl") {
				continue
			}
			lines := readIfTouchedInWindow(filepath.Join(dirPath, file), startMs)
			if lines == nil {
				continue
			}
			out = append(out, ClaudeUsageRowsFromLines(lines)...)
		}
	}
	return out
}

// scanGrokRowsByBackend groups session usage by the backend implied by each
// session's modelId + config.toml base_url.
func scanGrokRowsByBackend(startMs int64) map[string][]apiUsageRow {
	home := os.Getenv("GROK_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".grok")
	}
	root := filepath.Join(home, "sessions")
	out := make(map[string][]apiUsageRow)
	for _, group := range listDirSafe(root) {
		groupPath := filepath.Join(root, group)
		for _, sid := range listDirSafe(groupPath) {
			sessionDir := filepath.Join(groupPath, sid)
			lines := readIfTouchedInWindow(filepath.Join(sessionDir, "updates.jsonl"), startMs)
			if lines == nil {
				continue
			}
			rows := GrokUsageRowsFromLines(lines)
			if len(rows) == 0 {
				continue
			}
			modelID := ""
			if raw, err := os.ReadFile(filepath.Join(sessionDir, "summary.json")); err == nil {
				modelID = GrokModelIDFromSummaryJSON(string(raw))
			}
			if modelID == "" {
				modelID = GrokModelIDFromLines(lines)
			}
			// Prefer model stamped on the usage rows when present.
			if modelID == "" && rows[0].ModelID != "" {
				modelID = rows[0].ModelID
			}
			backendID := GrokBackendForModelID(modelID)
			out[backendID] = append(out[backendID], rows...)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// scanCodexRows groups rollouts by their session_meta.model_provider, the one
// place Codex records which backend a session actually used.
func scanCodexRows(startMs int64) map[string][]apiUsageRow {
	out := make(map[string][]apiUsageRow)
	for _, path := range ListNewestRolloutPaths(10_000) {
		lines := readIfTouchedInWindow(path, startMs)
		if lines == nil {
			continue
		}
		backendID := CodexProviderFromLines(lines)
		if backendID == "" {
			continue
		}
		rows := CodexUsageRowsFromLines(lines, CodexModelFromLines(lines))
		if len(rows) == 0 {
			continue
		}
		out[backendID] = append(out[backendID], rows...)
	}
	return out
}

// scanOMPPiRowsByBackend reads OMP / Pi session jsonl files touched in the
// window and groups assistant usage by message provider id.
func scanOMPPiRowsByBackend(paths []string, startMs int64) map[string][]apiUsageRow {
	out := make(map[string][]apiUsageRow)
	for _, path := range paths {
		lines := readIfTouchedInWindow(path, startMs)
		if lines == nil {
			continue
		}
		byBackend := OMPPiUsageRowsByBackendFromLines(lines)
		for backendID, rows := range byBackend {
			out[backendID] = append(out[backendID], rows...)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// fileHarnessPaneActivity scales each open pane's own session tokens against
// the backend total, matching how the other blocks compute their share row.
func fileHarnessPaneActivity(
	harnessID, backendID string,
	panes []OpenPaneSnapshot,
	backendRows []apiUsageRow,
	nowMs int64,
) *ProviderPaneActivity {
	startMs := WindowStartMs(nowMs, APIShareWindowMinutes)

	rawRows := make([]PaneTokenRow, 0, len(panes))
	for _, pane := range panes {
		lines := paneSessionLines(harnessID, pane)
		if lines == nil {
			continue
		}
		var rows []apiUsageRow
		switch harnessID {
		case "claude":
			if ResolveClaudeBackendID(claudeEnvForPane(pane)) != backendID {
				continue
			}
			rows = ClaudeUsageRowsFromLines(lines)
		case "grok":
			if resolveGrokBackendForPane(pane) != backendID {
				continue
			}
			rows = GrokUsageRowsFromLines(lines)
		case "codex":
			if CodexProviderFromLines(lines) != backendID {
				continue
			}
			rows = CodexUsageRowsFromLines(lines, "")
		case "omp", "pi":
			byBackend := OMPPiUsageRowsByBackendFromLines(lines)
			rows = byBackend[backendID]
		}
		var tokens float64
		for _, r := range rows {
			if r.CreatedMs >= startMs && r.CreatedMs <= nowMs {
				tokens += r.Tokens
			}
		}
		rawRows = append(rawRows, PaneTokenRow{PaneID: pane.PaneID, Label: pane.Label, Tokens: tokens})
	}
	if len(rawRows) == 0 {
		return nil
	}

	var backendTotal float64
	for _, r := range backendRows {
		if r.CreatedMs >= startMs && r.CreatedMs <= nowMs {
			backendTotal += r.Tokens
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

// paneSessionLines reads the transcript backing one open pane.
func paneSessionLines(harnessID string, pane OpenPaneSnapshot) []string {
	var sid, cwd *string
	if pane.SessionID != nil {
		sid = pane.SessionID
	}
	if pane.Cwd != nil {
		cwd = pane.Cwd
	}

	var path string
	switch harnessID {
	case "claude":
		if sid == nil {
			return nil
		}
		path = claude.ResolveTranscriptPathForSession(*sid)
	case "codex":
		path = codex.ResolveSessionFile(sid, cwd)
	case "grok":
		signals := grok.ResolveSignalsPath(sid, cwd)
		if signals == "" {
			return nil
		}
		path = strings.Replace(signals, "signals.json", "updates.jsonl", 1)
	case "omp":
		path = ompSessionPath(pane)
	case "pi":
		path = piSessionPath(pane)
	}
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(raw), "\n")
}
