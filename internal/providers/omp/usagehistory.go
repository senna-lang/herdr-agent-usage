/**
 * Read-only reader for OMP's own rate-limit window cache.
 *
 * OMP records the server-reported windows of every subscription account it
 * drives in agent.db `usage_history`, one row per (account, limit_id) per
 * refresh. Unlike a vendor CLI's session artifacts these rows exist for any
 * account OMP touches, so for a subscription whose vendor CLI never runs
 * this is the only local observation of the real windows.
 *
 * Secret material is never read: this file touches `usage_history` only,
 * never `auth_credentials.data`.
 */
package omp

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// UsageWindow is one recorded rate-limit window for one account.
type UsageWindow struct {
	// Provider is OMP's own provider id (anthropic, xai-oauth, opencode-go, …).
	Provider string
	// AccountKey identifies the account within that provider. Its shape is
	// OMP's, e.g. "email:a@b|org:<uuid>"; treat it as opaque.
	AccountKey string
	Email      string
	AccountID  string
	// LimitID is OMP's window id, e.g. "anthropic:5h" or "rolling-5h".
	LimitID     string
	Label       string
	WindowLabel string
	// UsedFraction is 0..1, and may exceed 1 for an exhausted window.
	UsedFraction float64
	Status       string
	// ResetsAtMs is epoch milliseconds; 0 when the window has no reset.
	ResetsAtMs int64
	// RecordedAtMs is when OMP observed this window.
	RecordedAtMs int64
}

// ResolveAgentDBPath returns USAGEBAR_OMP_AGENT_DB or ~/.omp/agent/agent.db.
func ResolveAgentDBPath() string {
	if override := strings.TrimSpace(os.Getenv("USAGEBAR_OMP_AGENT_DB")); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".omp", "agent", "agent.db")
}

// LatestUsageWindows returns the newest row per (provider, account, limit_id).
// It returns nil for any failure — a missing DB, an OMP old enough to predate
// the table, or an unreadable file are all "no observation", never an error
// the panel has to render.
func LatestUsageWindows(dbPath string) []UsageWindow {
	if dbPath == "" {
		return nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()

	// SQLite resolves the bare columns from the row holding MAX(recorded_at)
	// within each group, so this is the newest observation per window.
	rows, err := db.Query(`
		SELECT provider, account_key, COALESCE(email, ''), COALESCE(account_id, ''),
		       limit_id, label, COALESCE(window_label, ''),
		       COALESCE(used_fraction, 0), COALESCE(status, ''),
		       COALESCE(resets_at, 0), MAX(recorded_at)
		FROM usage_history
		GROUP BY lower(trim(provider)), account_key, lower(trim(limit_id))`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []UsageWindow
	for rows.Next() {
		var w UsageWindow
		if err := rows.Scan(&w.Provider, &w.AccountKey, &w.Email, &w.AccountID,
			&w.LimitID, &w.Label, &w.WindowLabel,
			&w.UsedFraction, &w.Status, &w.ResetsAtMs, &w.RecordedAtMs); err != nil {
			return nil
		}
		w.Provider = strings.ToLower(strings.TrimSpace(w.Provider))
		w.LimitID = strings.ToLower(strings.TrimSpace(w.LimitID))
		out = append(out, w)
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}
