/**
 * Fixture builder for OMP's agent.db, shared by the window-pool tests.
 */
package limits

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// usageRow is one usage_history row for a fixture DB. Field shapes mirror
// OMP's own schema, including epoch-millisecond timestamps.
type usageRow struct {
	RecordedAtMs int64
	Provider     string
	AccountKey   string
	Email        string
	AccountID    string
	LimitID      string
	Label        string
	WindowLabel  string
	UsedFraction float64
	Status       string
	ResetsAtMs   int64
}

// writeAgentDB builds an OMP agent.db holding rows and returns its path.
func writeAgentDB(t *testing.T, rows ...usageRow) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE usage_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		recorded_at INTEGER NOT NULL,
		provider TEXT NOT NULL,
		account_key TEXT NOT NULL,
		email TEXT,
		account_id TEXT,
		limit_id TEXT NOT NULL,
		label TEXT NOT NULL,
		window_label TEXT,
		used_fraction REAL,
		status TEXT,
		resets_at INTEGER)`); err != nil {
		t.Fatalf("create fixture table: %v", err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO usage_history
			(recorded_at, provider, account_key, email, account_id, limit_id,
			 label, window_label, used_fraction, status, resets_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			r.RecordedAtMs, r.Provider, r.AccountKey, r.Email, r.AccountID,
			r.LimitID, r.Label, r.WindowLabel, r.UsedFraction, r.Status,
			r.ResetsAtMs); err != nil {
			t.Fatalf("insert fixture row: %v", err)
		}
	}
	return path
}

// useAgentDB points the window pool at a fixture DB for one test.
func useAgentDB(t *testing.T, rows ...usageRow) string {
	t.Helper()
	path := writeAgentDB(t, rows...)
	t.Setenv("USAGEBAR_OMP_AGENT_DB", path)
	return path
}
