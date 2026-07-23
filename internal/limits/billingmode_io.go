/**
 * I/O adapters for billing-mode detection: reads local harness stores
 * (claude.json, codex rollouts, opencode.db, grok auth.json / config.toml)
 * and feeds the pure detectors in billingmode.go.
 */
package limits

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/fsutil"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/omp"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
	_ "modernc.org/sqlite"
)

// statusLineCacheFreshMs bounds how old a statusLine rate_limits cache may be
// to still count as subscription evidence (7 days — one full weekly window).
const statusLineCacheFreshMs = 7 * 24 * 60 * 60 * 1000

// DefaultBillingDeps returns production billing-mode resolvers. It resolves the
// profile snapshot once and shares it across both mode closures and the id
// list, so a BillingProviderFilter pass agrees on one profile set instead of
// re-resolving config/env per pane.
func DefaultBillingDeps() BillingDeps {
	profiles := ResolvedClaudeProfiles()
	ids := make([]string, len(profiles))
	for i, p := range profiles {
		ids[i] = p.ID
	}
	return BillingDeps{
		PaneMode: func(providerID string, pane OpenPaneSnapshot) BillingMode {
			return paneBillingModeWith(profiles, providerID, pane)
		},
		AccountMode: func(providerID string) BillingMode {
			return accountBillingModeWith(profiles, providerID)
		},
		ClaudeProfileIDs: ids,
		ResolvePane: func(pane OpenPaneSnapshot) (string, string, bool) {
			return resolveBilledPane(profiles, pane)
		},
	}
}

func resolveBilledPane(profiles []claude.ClaudeProfile, pane OpenPaneSnapshot) (providerID, harnessID string, ok bool) {
	harnessID, ok = agentToProvider[strings.ToLower(pane.Agent)]
	if !ok {
		return "", "", false
	}
	if harnessID == "claude" {
		providerID, ok = BuildClaudePaneProviderResolver(profiles)(pane)
		return providerID, harnessID, ok
	}
	if route, routed := paneSubscriptionRoute(harnessID, pane); routed {
		return route.CollectorProviderID, harnessID, true
	}
	return harnessID, harnessID, true
}

// paneBillingModeWith dispatches by provider id against an explicit profile
// snapshot. Claude-family ids (any configured profile, not just the literal
// "claude") resolve via that profile's own ConfigDir rather than the ambient
// CLAUDE_CONFIG_DIR — the read side (panel/sidebar) never sees that env var, so
// per-profile billing detection must thread the resolved profile's paths.
func paneBillingModeWith(profiles []claude.ClaudeProfile, providerID string, pane OpenPaneSnapshot) BillingMode {
	if profile, ok := profileByIDIn(profiles, providerID); ok {
		return claudePaneBillingModeIn(profile.ConfigDir, pane)
	}
	switch providerID {
	case "opencode":
		if _, ok := paneSubscriptionRoute(providerID, pane); ok {
			return BillingSubscription
		}
		if paneHasOAuthCredential(providerID, pane) {
			// This is a real subscription/login, but its collector is not
			// implemented yet (for example Copilot). Never turn it into a
			// fabricated API spend total merely because the collector is absent.
			return BillingUnknown
		}
		return opencodePaneBillingMode(pane)
	case "omp", "pi":
		if _, ok := paneSubscriptionRoute(providerID, pane); ok {
			// The session records a known subscription gateway.  Its quota is
			// owned by that gateway's account, not by the OMP/Pi harness.
			return BillingSubscription
		}
		if paneHasOAuthCredential(providerID, pane) {
			return BillingUnknown
		}
		// Other OMP / stock Pi sessions have no positive subscription route
		// evidence, so retain the backend-scoped PAYG behavior.
		return BillingPayAsYouGo
	case "codex":
		return codexPaneBillingMode(pane)
	case "grok":
		return grokPaneBillingMode(pane)
	default:
		return BillingUnknown
	}
}

// SubscriptionLimitsProviderID maps a pane's harness provider to the account
// provider that owns its subscription windows. OMP/Pi can run supported
// subscription gateways inside their own harness sessions.
func SubscriptionLimitsProviderID(providerID string, pane OpenPaneSnapshot) string {
	if route, ok := paneSubscriptionRoute(providerID, pane); ok {
		return route.CollectorProviderID
	}
	return providerID
}

// SubscriptionDisplayProviderID returns the provider label that should sit
// beside a subscription limit.  It preserves the gateway's distinct name
// (not the OMP/Pi harness and not the collector's internal id).
func SubscriptionDisplayProviderID(providerID string, pane OpenPaneSnapshot) string {
	if route, ok := paneSubscriptionRoute(providerID, pane); ok {
		return route.DisplayProviderID
	}
	return providerID
}

func accountBillingModeWith(profiles []claude.ClaudeProfile, providerID string) BillingMode {
	if profile, ok := profileByIDIn(profiles, providerID); ok {
		return claudeAccountBillingModeIn(profile.JSONPath, profile.LimitsCache, profile.ConfigDir)
	}
	switch providerID {
	case "grok":
		return grokAccountBillingMode()
	default:
		return BillingUnknown
	}
}

// opencodePaneBillingMode classifies a pane by the backend its session last
// used ("" when it cannot be resolved).
func opencodePaneBillingMode(pane OpenPaneSnapshot) BillingMode {
	backendID := opencodePaneBackendID(pane)
	if backendID == "" {
		return BillingUnknown
	}
	return OpenCodeBillingModeFromProviderID(&backendID)
}

// PaneBackendID returns the backend a pay-as-you-go pane is running
// ("deepseek", "openai", "anthropic"), or "" when the pane sits on a
// subscription plan or its backend cannot be resolved.
func PaneBackendID(providerID string, pane OpenPaneSnapshot) string {
	if PaneBillingMode(providerID, pane, DefaultBillingDeps()) != BillingPayAsYouGo {
		return ""
	}
	return payAsYouGoBackendID(providerID, pane)
}

// payAsYouGoBackendID names the backend of an already-classified
// pay-as-you-go pane.
//
// OpenCode / Codex record a per-session provider. Claude uses deployment
// env (settings + process); Grok joins session modelId with config.toml.
func payAsYouGoBackendID(providerID string, pane OpenPaneSnapshot) string {
	if profile, ok := claudeProfileByID(providerID); ok {
		return ResolveClaudeBackendID(loadClaudeEnvIn(profile.ConfigDir, cwdStr(pane)))
	}
	switch providerID {
	case "opencode":
		backendID := opencodePaneBackendID(pane)
		if backendID == openCodeGoBackendID {
			return ""
		}
		return backendID
	case "omp":
		return ompPaneBackendID(pane)
	case "pi":
		return piPaneBackendID(pane)
	case "codex":
		return codexPaneBackendID(pane)
	case "grok":
		return resolveGrokBackendForPane(pane)
	default:
		return ""
	}
}

// claudePaneBillingModeIn uses deployment env evidence (Bedrock/Vertex/…) from
// the given profile's ConfigDir (its own settings.json), not the ambient
// CLAUDE_CONFIG_DIR.
func claudePaneBillingModeIn(configDir string, pane OpenPaneSnapshot) BillingMode {
	return ClaudeBillingModeFromEnv(loadClaudeEnvIn(configDir, cwdStr(pane)))
}

// grokPaneBillingMode is PayAsYouGo when the pane's model points at a
// non-xAI base_url in config.toml.
func grokPaneBillingMode(pane OpenPaneSnapshot) BillingMode {
	return GrokBillingModeFromBackendID(resolveGrokBackendForPane(pane))
}

// claudeEnvForPane merges process env, user settings, and project settings
// for the pane cwd (later layers win).
func claudeEnvForPane(pane OpenPaneSnapshot) map[string]string {
	var cwd string
	if pane.Cwd != nil {
		cwd = *pane.Cwd
	}
	return loadClaudeEnv(cwd)
}

func loadClaudeEnv(cwd string) map[string]string {
	return loadClaudeEnvIn("", cwd)
}

// loadClaudeEnvIn is loadClaudeEnv scoped to an explicit profile ConfigDir.
// configDir="" keeps today's env/default lookup (ResolveClaudeUserSettingsPath),
// used by the single-account callers that predate multi-profile support.
func loadClaudeEnvIn(configDir, cwd string) map[string]string {
	return MergeClaudeEnv(
		claudeProcessEnv(),
		claudeSettingsEnv(resolveClaudeUserSettingsPathIn(configDir)),
		claudeSettingsEnv(filepath.Join(cwd, ".claude", "settings.json")),
		claudeSettingsEnv(filepath.Join(cwd, ".claude", "settings.local.json")),
	)
}

// resolveClaudeUserSettingsPathIn returns <configDir>/settings.json, or the
// env/default ResolveClaudeUserSettingsPath() when configDir is empty.
func resolveClaudeUserSettingsPathIn(configDir string) string {
	if configDir != "" {
		return filepath.Join(configDir, "settings.json")
	}
	return ResolveClaudeUserSettingsPath()
}

// claudeProcessEnv copies Claude deployment-related keys from the process
// environment (global shell exports apply to every pane).
func claudeProcessEnv() map[string]string {
	keys := []string{
		"CLAUDE_CODE_USE_BEDROCK",
		"CLAUDE_CODE_USE_VERTEX",
		"CLAUDE_CODE_USE_FOUNDRY",
		"CLAUDE_CODE_USE_MANTLE",
		"ANTHROPIC_BASE_URL",
		"ANTHROPIC_BEDROCK_BASE_URL",
		"ANTHROPIC_BEDROCK_MANTLE_BASE_URL",
		"ANTHROPIC_AWS_BASE_URL",
		"ANTHROPIC_VERTEX_BASE_URL",
		"ANTHROPIC_FOUNDRY_BASE_URL",
	}
	out := map[string]string{}
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func claudeSettingsEnv(path string) map[string]string {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return ClaudeEnvFromSettingsJSON(string(raw))
}

// ResolveClaudeUserSettingsPath returns ~/.claude/settings.json.
func ResolveClaudeUserSettingsPath() string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return filepath.Join(v, "settings.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// resolveGrokBackendForPane joins the pane session's model id with config.toml.
func resolveGrokBackendForPane(pane OpenPaneSnapshot) string {
	modelID := grokPaneModelID(pane)
	models, base := loadGrokModelConfig()
	return ResolveGrokBackendID(modelID, models, base)
}

func grokPaneModelID(pane OpenPaneSnapshot) string {
	var sid, cwd *string
	if pane.SessionID != nil {
		sid = pane.SessionID
	}
	if pane.Cwd != nil {
		cwd = pane.Cwd
	}
	signals := grok.ResolveSignalsPath(sid, cwd)
	if signals == "" {
		return ""
	}
	// Prefer summary.json (cheap, authoritative current model).
	summaryPath := strings.Replace(signals, "signals.json", "summary.json", 1)
	if raw, err := os.ReadFile(summaryPath); err == nil {
		if id := GrokModelIDFromSummaryJSON(string(raw)); id != "" {
			return id
		}
	}
	updatesPath := strings.Replace(signals, "signals.json", "updates.jsonl", 1)
	raw, err := os.ReadFile(updatesPath)
	if err != nil {
		return ""
	}
	return GrokModelIDFromLines(strings.Split(string(raw), "\n"))
}

type grokConfigCache struct {
	mu     sync.Mutex
	path   string
	mtime  int64
	models map[string]GrokModelConfig
	base   string
}

var globalGrokConfigCache grokConfigCache

// loadGrokModelConfig reads and caches ~/.grok/config.toml model tables.
func loadGrokModelConfig() (map[string]GrokModelConfig, string) {
	path := ResolveGrokConfigPath()
	st, err := os.Stat(path)
	if err != nil {
		return nil, ""
	}
	mtime := st.ModTime().UnixMilli()
	globalGrokConfigCache.mu.Lock()
	defer globalGrokConfigCache.mu.Unlock()
	var models map[string]GrokModelConfig
	var base string
	if globalGrokConfigCache.path == path && globalGrokConfigCache.mtime == mtime {
		models, base = globalGrokConfigCache.models, globalGrokConfigCache.base
	} else {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, ""
		}
		body := string(raw)
		models = ParseGrokModelConfigs(body)
		base = ParseGrokModelsBaseURL(body)
		globalGrokConfigCache.path = path
		globalGrokConfigCache.mtime = mtime
		globalGrokConfigCache.models = models
		globalGrokConfigCache.base = base
	}
	// Env override is applied after the file cache so it can change without
	// a config.toml mtime bump.
	if v := os.Getenv("GROK_MODELS_BASE_URL"); v != "" {
		base = v
	}
	return models, base
}

// ResolveGrokConfigPath returns $GROK_HOME/config.toml or ~/.grok/config.toml.
func ResolveGrokConfigPath() string {
	if home := os.Getenv("GROK_HOME"); home != "" {
		return filepath.Join(home, "config.toml")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".grok", "config.toml")
}

// AccountClaudeBackendID is the settings/process-derived backend for Claude
// when scanning account-scoped transcripts (no pane cwd).
func AccountClaudeBackendID() string {
	return ResolveClaudeBackendID(loadClaudeEnv(""))
}

// GrokBackendForModelID resolves a model id against the live config.toml.
func GrokBackendForModelID(modelID string) string {
	models, base := loadGrokModelConfig()
	return ResolveGrokBackendID(modelID, models, base)
}

// codexPaneBackendID reads session_meta.model_provider from the pane's rollout.
func codexPaneBackendID(pane OpenPaneSnapshot) string {
	lines := codexPaneRolloutLines(pane)
	if lines == nil {
		return ""
	}
	return CodexProviderFromLines(lines)
}

// codexPaneRolloutLines reads the pane's rollout in full. Unlike the
// rate-limit probe, which tail-scans for the freshest token_count,
// session_meta sits at the head of the file.
func codexPaneRolloutLines(pane OpenPaneSnapshot) []string {
	var sid, cwd *string
	if pane.SessionID != nil {
		sid = pane.SessionID
	}
	if pane.Cwd != nil {
		cwd = pane.Cwd
	}
	path := codex.ResolveSessionFile(sid, cwd)
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(raw), "\n")
}

// opencodePaneBackendID returns the backend providerID of the pane session's
// most recent assistant message (by session id, else newest session in the
// pane cwd). Empty when the DB, session, or message cannot be resolved.
func opencodePaneBackendID(pane OpenPaneSnapshot) string {
	dbPath := ResolveOpenCodeLimitsDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return ""
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return ""
	}
	defer db.Close()

	sessionID := resolvePaneSessionID(db, pane)
	if sessionID == "" {
		return ""
	}
	var providerID string
	if err := db.QueryRow(
		`SELECT json_extract(data, '$.providerID') FROM message
		 WHERE session_id = ?
		   AND json_valid(data)
		   AND json_extract(data, '$.role') = 'assistant'
		   AND json_extract(data, '$.providerID') IS NOT NULL
		 ORDER BY CAST(COALESCE(json_extract(data, '$.time.created'), time_created) AS INTEGER) DESC
		 LIMIT 1`,
		sessionID,
	).Scan(&providerID); err != nil {
		return ""
	}
	return providerID
}

// codexPaneBillingMode tail-scans the pane's own rollout for rate_limits.
func codexPaneBillingMode(pane OpenPaneSnapshot) BillingMode {
	var sid, cwd *string
	if pane.SessionID != nil {
		sid = pane.SessionID
	}
	if pane.Cwd != nil {
		cwd = pane.Cwd
	}
	path := codex.ResolveSessionFile(sid, cwd)
	if path == "" {
		return BillingUnknown
	}
	lines, err := fsutil.ReadLastNLines(path, codexTailScanBytes)
	if err != nil {
		return BillingUnknown
	}
	return CodexBillingModeFromLines(lines)
}

// claudeAccountBillingModeIn reads billing evidence from one profile's own
// claude.json / limits cache / ConfigDir, so each configured account's
// evidence comes from only its own files rather than the ambient default.

func claudeAccountBillingModeIn(jsonPath, limitsCachePath, configDir string) BillingMode {
	raw, err := os.ReadFile(jsonPath)
	mode := BillingUnknown
	if err == nil {
		mode = ClaudeBillingModeFromJSON(string(raw))
		if mode == BillingPayAsYouGo {
			// A fresh statusLine rate_limits cache is subscription evidence too
			// (the utilization cache can lag behind a new subscription login).
			nowMs := time.Now().UnixMilli()
			if cached := collectFromStatusLineCache(nowMs, limitsCachePath); cached != nil {
				if nowMs-cached.FetchedAtMs <= statusLineCacheFreshMs {
					mode = BillingSubscription
				}
			}
		}
	}
	// Deployment env (Bedrock/Vertex/…) is account-scoped when set in
	// user settings or the process environment.
	return CombineBillingModes(mode, ClaudeBillingModeFromEnv(loadClaudeEnvIn(configDir, "")))
}

func grokAccountBillingMode() BillingMode {
	raw, err := os.ReadFile(ResolveGrokAuthPath())
	if err != nil {
		return BillingUnknown
	}
	auth := ParseGrokAuthJSON(string(raw))
	if auth == nil {
		return BillingUnknown
	}
	return GrokBillingModeFromAuthMode(auth.AuthMode)
}

// ompPaneBackendID returns the latest assistant provider id from the OMP
// session jsonl path carried on the pane snapshot.
func ompPaneBackendID(pane OpenPaneSnapshot) string {
	path := ompSessionPath(pane)
	if path == "" {
		return ""
	}
	return omp.BackendIDForPath(path)
}

func ompPiPaneBackendID(providerID string, pane OpenPaneSnapshot) string {
	if providerID == "omp" {
		return ompPaneBackendID(pane)
	}
	if providerID == "pi" {
		return piPaneBackendID(pane)
	}
	return ""
}

func ompPiSubscriptionRoute(providerID string, pane OpenPaneSnapshot) (SubscriptionRoute, bool) {
	backendID := ompPiPaneBackendID(providerID, pane)
	credentialType := paneCredentialType(providerID, pane)
	return SubscriptionRouteForProviderAuth(backendID, credentialType)
}

func paneCredentialType(providerID string, pane OpenPaneSnapshot) string {
	switch providerID {
	case "omp":
		return omp.CredentialType(ompPiPaneBackendID(providerID, pane))
	case "pi":
		return omp.PiCredentialType(ompPiPaneBackendID(providerID, pane))
	case "opencode":
		backendID := opencodePaneBackendID(pane)
		return opencode.CredentialType(backendID)
	default:
		return ""
	}
}

func paneHasOAuthCredential(providerID string, pane OpenPaneSnapshot) bool {
	return strings.Contains(paneCredentialType(providerID, pane), "oauth")
}

// paneSubscriptionRoute resolves the subscription owner for harnesses that
// may delegate a turn to another provider account.
func paneSubscriptionRoute(providerID string, pane OpenPaneSnapshot) (SubscriptionRoute, bool) {
	switch providerID {
	case "omp", "pi":
		return ompPiSubscriptionRoute(providerID, pane)
	case "opencode":
		backendID := opencodePaneBackendID(pane)
		return SubscriptionRouteForProviderAuth(backendID, paneCredentialType(providerID, pane))
	default:
		return SubscriptionRoute{}, false
	}
}

// ompSessionPath resolves the OMP jsonl path from SessionID, else newest cwd session.
func ompSessionPath(pane OpenPaneSnapshot) string {
	if pane.SessionID != nil {
		if path := omp.SessionPathFromSnapshotValue(*pane.SessionID); path != "" {
			return path
		}
	}
	if pane.Cwd != nil {
		return omp.FindLatestOMPSessionForCwd(*pane.Cwd)
	}
	return ""
}

// piSessionPath resolves the stock Pi jsonl path from SessionID, else newest cwd session.
func piSessionPath(pane OpenPaneSnapshot) string {
	if pane.SessionID != nil {
		if path := omp.SessionPathFromSnapshotValue(*pane.SessionID); path != "" {
			return path
		}
	}
	if pane.Cwd != nil {
		return omp.FindLatestPiSessionForCwd(*pane.Cwd)
	}
	return ""
}

// piPaneBackendID returns the latest assistant provider id from the Pi
// session jsonl path carried on the pane snapshot.
func piPaneBackendID(pane OpenPaneSnapshot) string {
	path := piSessionPath(pane)
	if path == "" {
		return ""
	}
	return omp.BackendIDForPath(path)
}
