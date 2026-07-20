/**
 * I/O adapters for billing-mode detection: reads local harness stores
 * (claude.json, codex rollouts, opencode.db, grok auth.json) and feeds
 * the pure detectors in billingmode.go.
 */
package limits

import (
	"database/sql"
	"os"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/fsutil"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	_ "modernc.org/sqlite"
)

// statusLineCacheFreshMs bounds how old a statusLine rate_limits cache may be
// to still count as subscription evidence (7 days — one full weekly window).
const statusLineCacheFreshMs = 7 * 24 * 60 * 60 * 1000

// DefaultBillingDeps returns production billing-mode resolvers.
func DefaultBillingDeps() BillingDeps {
	return BillingDeps{
		PaneMode:    paneBillingModeDefault,
		AccountMode: accountBillingModeDefault,
	}
}

func paneBillingModeDefault(providerID string, pane OpenPaneSnapshot) BillingMode {
	switch providerID {
	case "opencode":
		return opencodePaneBillingMode(pane)
	case "codex":
		return codexPaneBillingMode(pane)
	default:
		// claude / grok billing is account-scoped; no session evidence.
		return BillingUnknown
	}
}

func accountBillingModeDefault(providerID string) BillingMode {
	switch providerID {
	case "claude":
		return claudeAccountBillingMode()
	case "grok":
		return grokAccountBillingMode()
	default:
		return BillingUnknown
	}
}

// opencodePaneBillingMode reads the latest assistant providerID of the
// pane's session (by session id, else newest session in the pane cwd).
func opencodePaneBillingMode(pane OpenPaneSnapshot) BillingMode {
	dbPath := ResolveOpenCodeLimitsDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return BillingUnknown
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return BillingUnknown
	}
	defer db.Close()

	sessionID := sessionIDStr(pane)
	if sessionID == "" {
		cwd := cwdStr(pane)
		if cwd == "" {
			return BillingUnknown
		}
		_ = db.QueryRow(
			`SELECT id FROM session WHERE directory = ? AND time_archived IS NULL ORDER BY time_updated DESC LIMIT 1`,
			cwd,
		).Scan(&sessionID)
		if sessionID == "" {
			return BillingUnknown
		}
	}
	var providerID string
	err = db.QueryRow(
		`SELECT json_extract(data, '$.providerID') FROM message
		 WHERE session_id = ?
		   AND json_valid(data)
		   AND json_extract(data, '$.role') = 'assistant'
		   AND json_extract(data, '$.providerID') IS NOT NULL
		 ORDER BY CAST(COALESCE(json_extract(data, '$.time.created'), time_created) AS INTEGER) DESC
		 LIMIT 1`,
		sessionID,
	).Scan(&providerID)
	if err != nil {
		return BillingUnknown
	}
	return OpenCodeBillingModeFromProviderID(&providerID)
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

func claudeAccountBillingMode() BillingMode {
	raw, err := os.ReadFile(ResolveClaudeJSONPath())
	if err != nil {
		return BillingUnknown
	}
	mode := ClaudeBillingModeFromJSON(string(raw))
	if mode == BillingPayAsYouGo {
		// A fresh statusLine rate_limits cache is subscription evidence too
		// (the utilization cache can lag behind a new subscription login).
		nowMs := time.Now().UnixMilli()
		if cached := collectFromStatusLineCache(nowMs, ResolveClaudeLimitsCachePath()); cached != nil {
			if nowMs-cached.FetchedAtMs <= statusLineCacheFreshMs {
				return BillingSubscription
			}
		}
	}
	return mode
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
