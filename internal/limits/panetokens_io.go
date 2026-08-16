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

	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/omp"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
	_ "modernc.org/sqlite"
)

// DefaultPaneActivityDeps resolves one profile snapshot per attach pass.
func DefaultPaneActivityDeps() PaneActivityDeps {
	profiles := ResolvedClaudeProfiles()
	codexProfiles := ResolvedCodexProfiles()
	grokProfiles := ResolvedGrokProfiles()
	openCodeProfiles := ResolvedOpenCodeProfiles()
	return PaneActivityDeps{
		TokensForPane: func(providerID string, pane OpenPaneSnapshot, startMs, endMs int64) float64 {
			return tokensForPaneWith(profiles, codexProfiles, grokProfiles, openCodeProfiles, providerID, pane, startMs, endMs)
		},
		TotalTokensForProvider: func(providerID string, startMs, endMs int64) float64 {
			return totalTokensForProviderWith(profiles, codexProfiles, grokProfiles, openCodeProfiles, providerID, startMs, endMs)
		},
		ResolvePaneProvider: BuildPaneActivityProviderResolver(profiles, codexProfiles, grokProfiles, openCodeProfiles),
	}
}

// BuildPaneActivityProviderResolver extends the normal harness resolver only
// for the Limits pane's subscription activity: OMP/Pi subscription gateways
// belong under their collector account. Keep this separate from
// BuildHarnessPaneProviderResolver, which sidebar updates use to retain the
// actual harness id before resolving its billing route.
func BuildPaneActivityProviderResolver(profiles []claude.ClaudeProfile, codexProfiles []codex.CodexProfile, grokProfiles []grok.GrokProfile, openCodeProfiles []opencode.OpenCodeProfile) PaneProviderResolver {
	base := BuildHarnessPaneProviderResolver(profiles, codexProfiles, grokProfiles, openCodeProfiles)
	return func(pane OpenPaneSnapshot) (string, bool) {
		if pane.Agent == "omp" || pane.Agent == "pi" {
			if route, ok := paneSubscriptionRoute(pane.Agent, pane); ok {
				return route.CollectorProviderID, true
			}
		}
		return base(pane)
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
	return BuildHarnessPaneProviderResolver(profiles, nil, nil, nil)
}

// BuildHarnessPaneProviderResolver attributes profile-capable harness panes
// from their own session stores and keeps static routing for other agents.
func BuildHarnessPaneProviderResolver(profiles []claude.ClaudeProfile, codexProfiles []codex.CodexProfile, grokProfiles []grok.GrokProfile, openCodeProfiles []opencode.OpenCodeProfile) PaneProviderResolver {
	claudeResolve := buildClaudeOnlyResolver(profiles)
	codexResolve := BuildCodexPaneProviderResolver(codexProfiles)
	grokResolve := BuildGrokPaneProviderResolver(grokProfiles)
	openCodeResolve := BuildOpenCodePaneProviderResolver(openCodeProfiles)
	return func(pane OpenPaneSnapshot) (string, bool) {
		switch pane.Agent {
		case "claude":
			return claudeResolve(pane)
		case "codex":
			return codexResolve(pane)
		case "grok":
			return grokResolve(pane)
		case "opencode":
			return openCodeResolve(pane)
		default:
			id, ok := agentToProvider[pane.Agent]
			return id, ok
		}
	}
}

func buildClaudeOnlyResolver(profiles []claude.ClaudeProfile) PaneProviderResolver {
	if len(profiles) == 1 {
		soleID := profiles[0].ID
		return func(pane OpenPaneSnapshot) (string, bool) {
			if pane.Agent == "claude" {
				return soleID, true
			}
			return "", false
		}
	}
	roots := make(map[string]string, len(profiles))
	for _, p := range profiles {
		roots[p.ID] = p.ProjectsRoot
	}
	return func(pane OpenPaneSnapshot) (string, bool) {
		if pane.Agent != "claude" {
			return "", false
		}
		return claude.ResolveProfileForSession(sessionIDStr(pane), roots)
	}
}

// BuildCodexPaneProviderResolver attributes Codex panes to the configured
// profile matching their rollout. A single profile short-circuits to that id.
func BuildCodexPaneProviderResolver(profiles []codex.CodexProfile) PaneProviderResolver {
	if len(profiles) == 0 {
		return func(pane OpenPaneSnapshot) (string, bool) {
			if pane.Agent == "codex" {
				return "codex", true
			}
			return "", false
		}
	}
	if len(profiles) == 1 {
		soleID := profiles[0].ID
		return func(pane OpenPaneSnapshot) (string, bool) {
			if pane.Agent == "codex" {
				return soleID, true
			}
			return "", false
		}
	}
	homes := make(map[string]string, len(profiles))
	for _, p := range profiles {
		homes[p.ID] = p.Home
	}
	return func(pane OpenPaneSnapshot) (string, bool) {
		if pane.Agent != "codex" {
			return "", false
		}
		return codex.ResolveProfileForSession(sessionIDStr(pane), homes)
	}
}

func BuildGrokPaneProviderResolver(profiles []grok.GrokProfile) PaneProviderResolver {
	if len(profiles) <= 1 {
		id := "grok"
		if len(profiles) == 1 {
			id = profiles[0].ID
		}
		return func(pane OpenPaneSnapshot) (string, bool) { return id, pane.Agent == "grok" }
	}
	return func(pane OpenPaneSnapshot) (string, bool) {
		if pane.Agent != "grok" {
			return "", false
		}
		match := ""
		for _, profile := range profiles {
			if grok.ResolveSignalsPathIn(profile.Home, pane.SessionID, pane.Cwd) == "" {
				continue
			}
			if match != "" {
				return "", false
			}
			match = profile.ID
		}
		return match, match != ""
	}
}

func BuildOpenCodePaneProviderResolver(profiles []opencode.OpenCodeProfile) PaneProviderResolver {
	if len(profiles) <= 1 {
		id := "opencode"
		if len(profiles) == 1 {
			id = profiles[0].ID
		}
		return func(pane OpenPaneSnapshot) (string, bool) { return id, pane.Agent == "opencode" }
	}
	return func(pane OpenPaneSnapshot) (string, bool) {
		if pane.Agent != "opencode" {
			return "", false
		}
		match := ""
		for _, profile := range profiles {
			if !openCodeProfileHasPane(profile, pane) {
				continue
			}
			if match != "" {
				return "", false
			}
			match = profile.ID
		}
		return match, match != ""
	}
}

func openCodeProfileHasPane(profile opencode.OpenCodeProfile, pane OpenPaneSnapshot) bool {
	path := opencode.ResolveOpenCodeDBPathIn(profile.DataDir)
	if path == "" {
		return false
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return false
	}
	defer db.Close()

	if sessionID := sessionIDStr(pane); sessionID != "" {
		var found string
		err := db.QueryRow(`SELECT id FROM session WHERE id = ?`, sessionID).Scan(&found)
		return err == nil
	}
	return resolvePaneSessionID(db, pane) != ""
}

// TokensForPaneDefault sums windowed tokens for one open pane, counting only
// subscription-billed traffic (opencode-go for OpenCode): it feeds the plan
// budget share under a provider's limits.
//
// providerID may be any configured Claude profile id (not just the literal
// "claude"), so Claude is dispatched by profile lookup rather than a switch
// case: each profile's tokens are read from only its own transcript root.
func TokensForPaneDefault(providerID string, pane OpenPaneSnapshot, startMs, endMs int64) float64 {
	return tokensForPaneWith(
		ResolvedClaudeProfiles(), ResolvedCodexProfiles(), ResolvedGrokProfiles(), ResolvedOpenCodeProfiles(),
		providerID, pane, startMs, endMs,
	)
}

func tokensForPaneWith(profiles []claude.ClaudeProfile, codexProfiles []codex.CodexProfile, grokProfiles []grok.GrokProfile, openCodeProfiles []opencode.OpenCodeProfile, providerID string, pane OpenPaneSnapshot, startMs, endMs int64) float64 {
	if profile, ok := openCodeProfileByIDIn(openCodeProfiles, providerID); ok && !profile.Implicit && pane.Agent == "opencode" {
		rows := opencodeSessionRowsForPaneIn(profile.DataDir, pane, startMs, endMs)
		return SumOpenCodeProviderTokensInWindow(rows, "opencode-go", startMs, endMs)
	}
	if pane.Agent == "opencode" {
		backendID := opencodePaneBackendID(pane)
		if route, ok := paneSubscriptionRoute("opencode", pane); ok && route.CollectorProviderID == providerID {
			return opencodeTokensForPane(pane, backendID, startMs, endMs)
		}
	}
	// An OMP/Pi subscription session belongs to the gateway's quota account,
	// but its local activity lives in the harness JSONL. Count only the gateway
	// recorded on this session; never mix earlier API backends into its share.
	if pane.Agent == "omp" || pane.Agent == "pi" {
		backendID := ompPiPaneBackendID(pane.Agent, pane)
		if route, ok := paneSubscriptionRoute(pane.Agent, pane); ok && route.CollectorProviderID == providerID {
			if pane.Agent == "omp" {
				tokens, _ := ompActivityForPaneBackend(pane, backendID, startMs, endMs)
				return tokens
			}
			tokens, _ := piActivityForPaneBackend(pane, backendID, startMs, endMs)
			return tokens
		}
	}
	if profile, ok := profileByIDIn(profiles, providerID); ok {
		return claudeTokensForPaneIn(profile.ProjectsRoot, pane, startMs, endMs)
	}
	if profile, ok := codexProfileByIDIn(codexProfiles, providerID); ok {
		return codexTokensForPaneIn(profile.Home, pane, startMs, endMs)
	}
	if pane.Agent == "grok" {
		for _, profile := range grokProfiles {
			if profile.ID == providerID {
				if profile.Implicit {
					return grokTokensForPane(pane, startMs, endMs)
				}
				return grokTokensForPaneIn(profile.Home, pane, startMs, endMs)
			}
		}
	}
	if pane.Agent == "opencode" {
		for _, profile := range openCodeProfiles {
			if profile.ID == providerID {
				if profile.Implicit {
					return opencodeTokensForPane(pane, "opencode-go", startMs, endMs)
				}
				rows := opencodeSessionRowsForPaneIn(profile.DataDir, pane, startMs, endMs)
				return SumOpenCodeProviderTokensInWindow(rows, "opencode-go", startMs, endMs)
			}
		}
	}
	switch providerID {
	case "opencode":
		return opencodeTokensForPane(pane, "opencode-go", startMs, endMs)
	case "omp":
		tokens, _ := ompActivityForPane(pane, startMs, endMs)
		return tokens
	case "pi":
		tokens, _ := piActivityForPane(pane, startMs, endMs)
		return tokens
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
	if profile, ok := openCodeProfileByIDIn(ResolvedOpenCodeProfiles(), providerID); ok {
		if profile.Implicit {
			backendID := payAsYouGoBackendID(providerID, pane)
			return opencodeActivityForPane(pane, backendID, 0, nowMs)
		}
		backendID := opencodePaneBackendIDIn(profile.DataDir, pane)
		rows := opencodeSessionRowsForPaneIn(profile.DataDir, pane, 0, nowMs)
		return SumOpenCodeActivityInWindow(rows, backendID, 0, nowMs)
	}
	if providerID == "opencode" {
		backendID := payAsYouGoBackendID(providerID, pane)
		return opencodeActivityForPane(pane, backendID, 0, nowMs)
	}
	if providerID == "omp" {
		return ompActivityForPaneBackend(pane, ompPaneBackendID(pane), 0, nowMs)
	}
	if providerID == "pi" {
		return piActivityForPaneBackend(pane, piPaneBackendID(pane), 0, nowMs)
	}
	return TokensForPaneAnyBackend(providerID, pane, 0, nowMs), 0
}

// TokensForPaneAnyBackend sums a pane's tokens in [startMs, endMs] across any
// backend, unlike TokensForPaneDefault which restricts OpenCode to the
// opencode-go subscription gateway for plan-budget accounting.
func TokensForPaneAnyBackend(providerID string, pane OpenPaneSnapshot, startMs, endMs int64) float64 {
	if profile, ok := openCodeProfileByIDIn(ResolvedOpenCodeProfiles(), providerID); ok {
		if profile.Implicit {
			return opencodeTokensForPane(pane, "", startMs, endMs)
		}
		rows := opencodeSessionRowsForPaneIn(profile.DataDir, pane, startMs, endMs)
		return SumOpenCodeProviderTokensInWindow(rows, "", startMs, endMs)
	}
	if providerID == "opencode" {
		return opencodeTokensForPane(pane, "", startMs, endMs)
	}
	return TokensForPaneDefault(providerID, pane, startMs, endMs)
}

// TotalTokensForProviderDefault sums windowed tokens across all sessions on
// disk. Claude profile ids are dispatched by lookup (see TokensForPaneDefault)
// so each profile's total is scanned from only its own transcript root.
func TotalTokensForProviderDefault(providerID string, startMs, endMs int64) float64 {
	return totalTokensForProviderWith(
		ResolvedClaudeProfiles(),
		ResolvedCodexProfiles(),
		ResolvedGrokProfiles(),
		ResolvedOpenCodeProfiles(),
		providerID,
		startMs,
		endMs,
	)
}

// totalTokensForProviderWith is TotalTokensForProviderDefault dispatched against
// an explicit profile snapshot (see DefaultPaneActivityDeps).
func totalTokensForProviderWith(profiles []claude.ClaudeProfile, codexProfiles []codex.CodexProfile, grokProfiles []grok.GrokProfile, openCodeProfiles []opencode.OpenCodeProfile, providerID string, startMs, endMs int64) float64 {
	routed := routedSubscriptionTotal(providerID, startMs, endMs)
	if profile, ok := profileByIDIn(profiles, providerID); ok {
		return claudeTotalIn(profile.ProjectsRoot, startMs, endMs) + routed
	}
	if profile, ok := codexProfileByIDIn(codexProfiles, providerID); ok {
		return codexTotalIn(profile.Home, startMs, endMs) + routed
	}
	if profile, ok := grokProfileByIDIn(grokProfiles, providerID); ok {
		if profile.Implicit {
			return grokTotal(startMs, endMs) + routed
		}
		return grokTotalIn(profile.Home, startMs, endMs) + routed
	}
	if profile, ok := openCodeProfileByIDIn(openCodeProfiles, providerID); ok {
		if profile.Implicit {
			return openCodeTotal(startMs, endMs) + routed
		}
		return openCodeTotalIn(profile.DataDir, startMs, endMs) + routed
	}

	switch providerID {
	case "opencode":
		return openCodeTotal(startMs, endMs) + routed
	case "grok":
		return grokTotal(startMs, endMs) + routed
	default:
		return routed
	}
}

// routedSubscriptionTotal adds activity recorded by harnesses other than the
// collector's native one. This keeps the provider block authoritative when
// OMP/Pi/OpenCode all spend the same subscription account.
func routedSubscriptionTotal(providerID string, startMs, endMs int64) float64 {
	var total float64
	for _, source := range []struct {
		harness string
		paths   []string
	}{
		{"omp", omp.ListAllOMPSessionFiles()},
		{"pi", omp.ListAllPiSessionFiles()},
	} {
		byBackend := scanOMPPiRowsByBackend(source.paths, startMs)
		for backendID, rows := range byBackend {
			credentialType := ""
			if source.harness == "omp" {
				credentialType = omp.CredentialType(backendID)
			} else {
				credentialType = omp.PiCredentialType(backendID)
			}
			route, ok := SubscriptionRouteForProviderAuth(backendID, credentialType)
			if !ok || route.CollectorProviderID != providerID {
				continue
			}
			for _, row := range rows {
				if row.CreatedMs >= startMs && row.CreatedMs <= endMs {
					total += row.Tokens
				}
			}
		}
	}
	// OpenCode Go is already included by openCodeTotal; only add OpenCode
	// sessions routed to a different subscription collector (e.g. Codex).
	if providerID != "opencode" {
		total += openCodeRoutedSubscriptionTotal(providerID, startMs, endMs)
	}
	return total
}

func openCodeRoutedSubscriptionTotal(providerID string, startMs, endMs int64) float64 {
	dbPath := ResolveOpenCodeLimitsDBPath()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return 0
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT DISTINCT json_extract(data, '$.providerID') FROM message
		 WHERE time_created >= ? AND time_created <= ? AND json_valid(data)
		   AND json_extract(data, '$.role') = 'assistant'`, startMs, endMs)
	if err != nil {
		return 0
	}
	defer rows.Close()
	var backendIDs []string
	for rows.Next() {
		var backendID string
		if rows.Scan(&backendID) == nil && backendID != "" {
			backendIDs = append(backendIDs, backendID)
		}
	}
	var total float64
	for _, backendID := range backendIDs {
		route, ok := SubscriptionRouteForProviderAuth(backendID, opencode.CredentialType(backendID))
		if !ok || route.CollectorProviderID != providerID {
			continue
		}
		query := `SELECT data, time_created FROM message
		 WHERE time_created >= ? AND time_created <= ? AND json_valid(data)
		   AND json_extract(data, '$.role') = 'assistant'
		   AND json_extract(data, '$.providerID') = ?`
		messageRows, err := db.Query(query, startMs, endMs, backendID)
		if err != nil {
			continue
		}
		var list []OpenCodeTokenRow
		for messageRows.Next() {
			var data string
			var created int64
			if messageRows.Scan(&data, &created) == nil {
				list = append(list, OpenCodeTokenRow{Data: data, TimeCreated: created})
			}
		}
		messageRows.Close()
		total += SumOpenCodeProviderTokensInWindow(list, backendID, startMs, endMs)
	}
	return total
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
	path := claude.ResolveTranscriptPathForSessionIn(root, sid)
	if path == "" {
		return 0
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return SumClaudeTokensInWindow(strings.Split(string(raw), "\n"), startMs, endMs)
}

func codexTokensForPaneIn(home string, pane OpenPaneSnapshot, startMs, endMs int64) float64 {
	var sid, cwd *string
	if pane.SessionID != nil {
		sid = pane.SessionID
	}
	if pane.Cwd != nil {
		cwd = pane.Cwd
	}
	path := codex.ResolveSessionFileIn(home, sid, cwd)
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

func opencodeSessionRowsForPaneIn(dataDir string, pane OpenPaneSnapshot, startMs, endMs int64) []OpenCodeTokenRow {
	dbPath := opencode.ResolveOpenCodeDBPathIn(dataDir)
	if dbPath == "" {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	sessionID := resolvePaneSessionID(db, pane)
	if sessionID == "" {
		return nil
	}
	rows, err := db.Query(`SELECT data, time_created FROM message WHERE session_id = ? AND time_created >= ? AND time_created <= ?`, sessionID, startMs, endMs)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var list []OpenCodeTokenRow
	for rows.Next() {
		var data string
		var createdAt int64
		if rows.Scan(&data, &createdAt) == nil {
			list = append(list, OpenCodeTokenRow{Data: data, TimeCreated: createdAt})
		}
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

func grokTokensForPaneIn(home string, pane OpenPaneSnapshot, startMs, endMs int64) float64 {
	signals := grok.ResolveSignalsPathIn(home, pane.SessionID, pane.Cwd)
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

func codexTotalIn(home string, startMs, endMs int64) float64 {
	var sum float64
	for _, path := range ListNewestRolloutPathsIn(home, 10_000) {
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

func openCodeTotalIn(dataDir string, startMs, endMs int64) float64 {
	dbPath := opencode.ResolveOpenCodeDBPathIn(dataDir)
	if dbPath == "" {
		return 0
	}
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
		var createdAt int64
		if rows.Scan(&data, &createdAt) == nil {
			list = append(list, OpenCodeTokenRow{Data: data, TimeCreated: createdAt})
		}
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

func grokTotalIn(home string, startMs, endMs int64) float64 {
	root := filepath.Join(home, "sessions")
	var sum float64
	for _, group := range listDirSafe(root) {
		groupPath := filepath.Join(root, group)
		for _, sessionID := range listDirSafe(groupPath) {
			updates := filepath.Join(groupPath, sessionID, "updates.jsonl")
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

func ompActivityForPane(pane OpenPaneSnapshot, startMs, endMs int64) (tokens float64, costUSD float64) {
	path := ompSessionPath(pane)
	if path == "" {
		return 0, 0
	}
	return omp.ActivityForPath(path, startMs, endMs)
}

func ompActivityForPaneBackend(pane OpenPaneSnapshot, backendID string, startMs, endMs int64) (tokens float64, costUSD float64) {
	path := ompSessionPath(pane)
	if path == "" || backendID == "" {
		return 0, 0
	}
	return omp.ActivityForProviderPath(path, backendID, startMs, endMs)
}

func piActivityForPane(pane OpenPaneSnapshot, startMs, endMs int64) (tokens float64, costUSD float64) {
	path := piSessionPath(pane)
	if path == "" {
		return 0, 0
	}
	return omp.ActivityForPath(path, startMs, endMs)
}

func piActivityForPaneBackend(pane OpenPaneSnapshot, backendID string, startMs, endMs int64) (tokens float64, costUSD float64) {
	path := piSessionPath(pane)
	if path == "" || backendID == "" {
		return 0, 0
	}
	return omp.ActivityForProviderPath(path, backendID, startMs, endMs)
}
