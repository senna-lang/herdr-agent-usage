/**
 * I/O adapters that feed AttachPaneActivity: per-pane and provider-total
 * windowed token sums for open sessions and all sessions on disk.
 */
package limits

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/claude"
	claudeprovider "github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	_ "modernc.org/sqlite"
)

// DefaultPaneActivityDeps returns production token collectors, wired to
// resolve Claude panes by profile (session-transcript match) when more than
// one Claude profile is configured.
func DefaultPaneActivityDeps() PaneActivityDeps {
	// Resolve the profile snapshot once and share it across all three deps, so
	// the whole AttachPaneActivity pass agrees on one profile set instead of
	// the resolver and the token/total dispatch each re-resolving (which could
	// also skew if config changed mid-pass).
	profiles := ResolvedClaudeProfiles()
	return PaneActivityDeps{
		TokensForPane: func(providerID string, pane OpenPaneSnapshot, startMs, endMs int64) float64 {
			return tokensForPaneWith(profiles, providerID, pane, startMs, endMs)
		},
		TotalTokensForProvider: func(providerID string, startMs, endMs int64) float64 {
			return totalTokensForProviderWith(profiles, providerID, startMs, endMs)
		},
		ResolvePaneProvider: BuildClaudePaneProviderResolver(profiles),
	}
}

// BuildClaudePaneProviderResolver attributes Claude panes to the specific
// configured profile matching their session transcript, and other agents via
// the static agentToProvider map.
//
// A single Claude profile (whether the synthesized default or one explicitly
// configured profile, which need not be id "claude") short-circuits to that
// profile's id directly rather than session matching — same cost as today,
// but correct even when the lone profile has a custom id.
func BuildClaudePaneProviderResolver(profiles []claude.ClaudeProfile) PaneProviderResolver {
	if len(profiles) == 1 {
		soleID := profiles[0].ID
		return func(pane OpenPaneSnapshot) (string, bool) {
			if pane.Agent == "claude" {
				return soleID, true
			}
			id, ok := agentToProvider[pane.Agent]
			return id, ok
		}
	}
	roots := make(map[string]string, len(profiles))
	for _, p := range profiles {
		roots[p.ID] = p.ProjectsRoot
	}
	return func(pane OpenPaneSnapshot) (string, bool) {
		if pane.Agent != "claude" {
			id, ok := agentToProvider[pane.Agent]
			return id, ok
		}
		return claudeprovider.ResolveProfileForSession(sessionIDStr(pane), roots)
	}
}

// TokensForPaneDefault sums windowed tokens for one open pane, counting only
// subscription-billed traffic (opencode-go for OpenCode): it feeds the plan
// budget share under a provider's limits.
//
// providerID may be any configured Claude profile id (not just the literal
// "claude"), so Claude is dispatched by profile lookup rather than a switch
// case: each profile's tokens are read from only its own transcript root.
func TokensForPaneDefault(providerID string, pane OpenPaneSnapshot, startMs, endMs int64) float64 {
	return tokensForPaneWith(ResolvedClaudeProfiles(), providerID, pane, startMs, endMs)
}

// tokensForPaneWith is TokensForPaneDefault dispatched against an explicit
// profile snapshot (see DefaultPaneActivityDeps): Claude ids resolve their
// transcript root from profiles, other agents via the static switch.
func tokensForPaneWith(profiles []claude.ClaudeProfile, providerID string, pane OpenPaneSnapshot, startMs, endMs int64) float64 {
	if profile, ok := profileByIDIn(profiles, providerID); ok {
		return claudeTokensForPaneIn(profile.ProjectsRoot, pane, startMs, endMs)
	}
	switch providerID {
	case "codex":
		return codexTokensForPane(pane, startMs, endMs)
	case "opencode":
		return opencodeTokensForPane(pane, "opencode-go", startMs, endMs)
	case "grok":
		return grokTokensForPane(pane, startMs, endMs)
	default:
		return 0
	}
}

// PaneTotalUsage sums what the pane spent on its pay-as-you-go backend —
// tokens and, where available, USD cost. Pay-as-you-go has no rolling quota
// to report against, so the sidebar shows the pane's whole-session total
// instead of a windowed rate.
//
// An OpenCode session can switch backends mid-way (e.g. opencode-go then
// deepseek); the total is scoped to the pane's current backend so it lines up
// with the "$provider" label and excludes the subscription-gateway spend
// already covered by that provider's limit row. Codex/Claude/Grok keep one
// backend per session, so their per-session read is already backend-scoped.
// costUSD is 0 when the harness records no local cost (Codex/Claude/Grok)
// rather than when spend was genuinely zero.
func PaneTotalUsage(providerID string, pane OpenPaneSnapshot, nowMs int64) (tokens float64, costUSD float64) {
	if providerID == "opencode" {
		backendID := payAsYouGoBackendID(providerID, pane)
		return opencodeActivityForPane(pane, backendID, 0, nowMs)
	}
	return TokensForPaneAnyBackend(providerID, pane, 0, nowMs), 0
}

// TokensForPaneAnyBackend sums a pane's tokens in [startMs, endMs] across any
// backend, unlike TokensForPaneDefault which restricts OpenCode to the
// opencode-go subscription gateway for plan-budget accounting.
func TokensForPaneAnyBackend(providerID string, pane OpenPaneSnapshot, startMs, endMs int64) float64 {
	if providerID == "opencode" {
		return opencodeTokensForPane(pane, "", startMs, endMs)
	}
	return TokensForPaneDefault(providerID, pane, startMs, endMs)
}

// TotalTokensForProviderDefault sums windowed tokens across all sessions on
// disk. Claude profile ids are dispatched by lookup (see TokensForPaneDefault)
// so each profile's total is scanned from only its own transcript root.
func TotalTokensForProviderDefault(providerID string, startMs, endMs int64) float64 {
	return totalTokensForProviderWith(ResolvedClaudeProfiles(), providerID, startMs, endMs)
}

// totalTokensForProviderWith is TotalTokensForProviderDefault dispatched against
// an explicit profile snapshot (see DefaultPaneActivityDeps).
func totalTokensForProviderWith(profiles []claude.ClaudeProfile, providerID string, startMs, endMs int64) float64 {
	if profile, ok := profileByIDIn(profiles, providerID); ok {
		return claudeTotalIn(profile.ProjectsRoot, startMs, endMs)
	}
	switch providerID {
	case "codex":
		return codexTotal(startMs, endMs)
	case "opencode":
		return openCodeTotal(startMs, endMs)
	case "grok":
		return grokTotal(startMs, endMs)
	default:
		return 0
	}
}

func sessionIDStr(pane OpenPaneSnapshot) string {
	if pane.SessionID == nil {
		return ""
	}
	return *pane.SessionID
}

func cwdStr(pane OpenPaneSnapshot) string {
	if pane.Cwd == nil {
		return ""
	}
	return *pane.Cwd
}

// claudeTokensForPaneIn sums one pane's windowed tokens from an explicit
// projects root, so a pane's tokens are read from only its resolved profile's
// root (see claudeProfileByID dispatch in TokensForPaneDefault).
func claudeTokensForPaneIn(root string, pane OpenPaneSnapshot, startMs, endMs int64) float64 {
	sid := sessionIDStr(pane)
	if sid == "" {
		return 0
	}
	path := claudeprovider.ResolveTranscriptPathForSessionIn(root, sid)
	if path == "" {
		return 0
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return SumClaudeTokensInWindow(strings.Split(string(raw), "\n"), startMs, endMs)
}

func codexTokensForPane(pane OpenPaneSnapshot, startMs, endMs int64) float64 {
	var sid, cwd *string
	if pane.SessionID != nil {
		sid = pane.SessionID
	}
	if pane.Cwd != nil {
		cwd = pane.Cwd
	}
	path := codex.ResolveSessionFile(sid, cwd)
	if path == "" {
		return 0
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return SumCodexTokensInWindow(strings.Split(string(raw), "\n"), startMs, endMs)
}

// opencodeSessionRowsForPane loads the pane's session message rows within
// the window (by session id, else newest session in the pane cwd).
func opencodeSessionRowsForPane(pane OpenPaneSnapshot, startMs, endMs int64) []OpenCodeTokenRow {
	dbPath := ResolveOpenCodeLimitsDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()

	sessionID := sessionIDStr(pane)
	if sessionID == "" {
		cwd := cwdStr(pane)
		if cwd == "" {
			return nil
		}
		_ = db.QueryRow(
			`SELECT id FROM session WHERE directory = ? AND time_archived IS NULL ORDER BY time_updated DESC LIMIT 1`,
			cwd,
		).Scan(&sessionID)
		if sessionID == "" {
			return nil
		}
	}
	rows, err := db.Query(
		`SELECT data, time_created FROM message WHERE session_id = ? AND time_created >= ? AND time_created <= ?`,
		sessionID, startMs, endMs,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var list []OpenCodeTokenRow
	for rows.Next() {
		var data string
		var tc int64
		if err := rows.Scan(&data, &tc); err != nil {
			continue
		}
		list = append(list, OpenCodeTokenRow{Data: data, TimeCreated: tc})
	}
	return list
}

// opencodeTokensForPane sums the pane session's windowed tokens for one
// backend providerID ("" = all backends).
func opencodeTokensForPane(pane OpenPaneSnapshot, backendID string, startMs, endMs int64) float64 {
	rows := opencodeSessionRowsForPane(pane, startMs, endMs)
	return SumOpenCodeProviderTokensInWindow(rows, backendID, startMs, endMs)
}

// opencodeActivityForPane sums the pane session's windowed tokens and USD
// cost for one backend providerID ("" = all backends), in one DB round trip.
func opencodeActivityForPane(pane OpenPaneSnapshot, backendID string, startMs, endMs int64) (tokens float64, costUSD float64) {
	rows := opencodeSessionRowsForPane(pane, startMs, endMs)
	return SumOpenCodeActivityInWindow(rows, backendID, startMs, endMs)
}

func grokTokensForPane(pane OpenPaneSnapshot, startMs, endMs int64) float64 {
	var sid, cwd *string
	if pane.SessionID != nil {
		sid = pane.SessionID
	}
	if pane.Cwd != nil {
		cwd = pane.Cwd
	}
	signals := grok.ResolveSignalsPath(sid, cwd)
	if signals == "" {
		return 0
	}
	updatesPath := strings.Replace(signals, "signals.json", "updates.jsonl", 1)
	raw, err := os.ReadFile(updatesPath)
	if err != nil {
		return 0
	}
	return SumGrokTokensInWindow(strings.Split(string(raw), "\n"), startMs, endMs)
}

func mtimeMsOrNull(path string) int64 {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return -1
	}
	return st.ModTime().UnixMilli()
}

func readIfTouchedInWindow(path string, startMs int64) []string {
	mt := mtimeMsOrNull(path)
	if mt < 0 || mt < startMs {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(raw), "\n")
}

func listDirSafe(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func claudeProjectsRoot() string {
	if v := os.Getenv("CLAUDE_PROJECTS_ROOT"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// claudeTotalIn sums windowed tokens across all sessions under an explicit
// projects root, so each Claude profile's activity total is scanned from only
// its own root (see claudeProfileByID dispatch in TotalTokensForProviderDefault).
func claudeTotalIn(root string, startMs, endMs int64) float64 {
	var sum float64
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
			sum += SumClaudeTokensInWindow(lines, startMs, endMs)
		}
	}
	return sum
}

func codexTotal(startMs, endMs int64) float64 {
	var sum float64
	for _, path := range ListNewestRolloutPaths(10_000) {
		lines := readIfTouchedInWindow(path, startMs)
		if lines == nil {
			continue
		}
		sum += SumCodexTokensInWindow(lines, startMs, endMs)
	}
	return sum
}

func openCodeTotal(startMs, endMs int64) float64 {
	dbPath := ResolveOpenCodeLimitsDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return 0
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return 0
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT data, time_created FROM message
		 WHERE time_created >= ? AND time_created <= ?
		   AND json_valid(data)
		   AND json_extract(data, '$.role') = 'assistant'`,
		startMs, endMs,
	)
	if err != nil {
		return 0
	}
	defer rows.Close()
	var list []OpenCodeTokenRow
	for rows.Next() {
		var data string
		var tc int64
		if err := rows.Scan(&data, &tc); err != nil {
			continue
		}
		list = append(list, OpenCodeTokenRow{Data: data, TimeCreated: tc})
	}
	return SumOpenCodeProviderTokensInWindow(list, "opencode-go", startMs, endMs)
}

func grokTotal(startMs, endMs int64) float64 {
	home := os.Getenv("GROK_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".grok")
	}
	root := filepath.Join(home, "sessions")
	var sum float64
	for _, group := range listDirSafe(root) {
		groupPath := filepath.Join(root, group)
		for _, sid := range listDirSafe(groupPath) {
			updates := filepath.Join(groupPath, sid, "updates.jsonl")
			lines := readIfTouchedInWindow(updates, startMs)
			if lines == nil {
				continue
			}
			sum += SumGrokTokensInWindow(lines, startMs, endMs)
		}
	}
	return sum
}

// CollectAndAttachPaneActivity attaches activity using DefaultPaneActivityDeps.
func CollectAndAttachPaneActivity(providers []ProviderLimits, openPanes []OpenPaneSnapshot, nowMs int64) []ProviderLimits {
	return AttachPaneActivity(providers, openPanes, nowMs, DefaultPaneActivityDeps())
}
