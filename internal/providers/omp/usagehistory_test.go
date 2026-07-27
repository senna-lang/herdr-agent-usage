/**
 * Tests for the read-only OMP agent.db usage-window reader.
 *
 * The contract these defend: the reader yields exactly one window per
 * (provider, account_key, limit_id) — the newest observation — with provider
 * and limit ids normalised, NULL columns degraded to zero values, and every
 * failure mode reported as "no observation" (nil) rather than an error or a
 * panic the panel would have to render.
 */
package omp

import (
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// usageRow is one usage_history row for a fixture DB. The nullable columns
// are typed `any` so a test can insert a real SQL NULL by leaving them nil.
type usageRow struct {
	RecordedAtMs int64
	Provider     string
	AccountKey   string
	Email        any
	AccountID    any
	LimitID      string
	Label        string
	WindowLabel  any
	UsedFraction any
	Status       any
	ResetsAtMs   any
}

// writeUsageDB builds an agent.db with OMP's real usage_history schema.
func writeUsageDB(t *testing.T, rows ...usageRow) string {
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

func TestResolveAgentDBPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("user home dir: %v", err)
	}
	fallback := filepath.Join(home, ".omp", "agent", "agent.db")

	tests := []struct {
		name string
		env  string
		want string
	}{
		{"override wins", "/tmp/usagebar-test/agent.db", "/tmp/usagebar-test/agent.db"},
		{"override is trimmed", "  /tmp/usagebar-test/agent.db  ", "/tmp/usagebar-test/agent.db"},
		{"unset falls back to ~/.omp", "", fallback},
		{"blank override falls back", "   ", fallback},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("USAGEBAR_OMP_AGENT_DB", tc.env)
			if got := ResolveAgentDBPath(); got != tc.want {
				t.Fatalf("ResolveAgentDBPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLatestUsageWindowsNewestPerWindow(t *testing.T) {
	const (
		provider = "anthropic"
		account  = "oauth|account:f3a39003-3c6f-438d-8b25-cf345f085528|email:asrsena3302@gmail.com|org:1ee9778e-5379-4a19-a14a-2625ea6002d5"
		limitID  = "anthropic:5h"
	)
	// Inserted out of chronological order so a reader that simply takes the
	// first or last inserted row cannot pass by accident.
	path := writeUsageDB(t,
		usageRow{RecordedAtMs: 1_700_000_100_000, Provider: provider, AccountKey: account,
			Email: "asrsena3302@gmail.com", LimitID: limitID, Label: "oldest",
			UsedFraction: 0.10, Status: "stale", ResetsAtMs: int64(1_700_000_400_000)},
		usageRow{RecordedAtMs: 1_700_000_300_000, Provider: provider, AccountKey: account,
			Email: "asrsena3302@gmail.com", LimitID: limitID, Label: "newest",
			UsedFraction: 0.75, Status: "allowed", ResetsAtMs: int64(1_700_001_200_000)},
		usageRow{RecordedAtMs: 1_700_000_200_000, Provider: provider, AccountKey: account,
			Email: "asrsena3302@gmail.com", LimitID: limitID, Label: "middle",
			UsedFraction: 0.20, Status: "stale", ResetsAtMs: int64(1_700_000_800_000)},
	)

	got := LatestUsageWindows(path)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 collapsed window; got %+v", len(got), got)
	}
	w := got[0]
	if w.RecordedAtMs != 1_700_000_300_000 {
		t.Errorf("RecordedAtMs = %d, want the newest 1700000300000", w.RecordedAtMs)
	}
	// Every non-aggregated column must come from the newest row too, not from
	// an arbitrary member of the group.
	if !closeEnough(w.UsedFraction, 0.75) {
		t.Errorf("UsedFraction = %v, want the newest row's 0.75", w.UsedFraction)
	}
	if w.Label != "newest" {
		t.Errorf("Label = %q, want %q", w.Label, "newest")
	}
	if w.Status != "allowed" {
		t.Errorf("Status = %q, want %q", w.Status, "allowed")
	}
	if w.ResetsAtMs != 1_700_001_200_000 {
		t.Errorf("ResetsAtMs = %d, want the newest row's 1700001200000", w.ResetsAtMs)
	}
	if w.Provider != provider || w.AccountKey != account || w.LimitID != limitID {
		t.Errorf("identity = (%q, %q, %q), want (%q, %q, %q)",
			w.Provider, w.AccountKey, w.LimitID, provider, account, limitID)
	}
	if w.Email != "asrsena3302@gmail.com" {
		t.Errorf("Email = %q, want %q", w.Email, "asrsena3302@gmail.com")
	}
}

func TestLatestUsageWindowsGroupsAreIndependent(t *testing.T) {
	const (
		oauthKey  = "oauth|account:f3a39003-3c6f-438d-8b25-cf345f085528|email:asrsena3302@gmail.com|org:1ee9778e-5379-4a19-a14a-2625ea6002d5"
		secretKey = "oauth|secret:c5f777162aca2d15"
		apiKey    = "api_key|secret:e9554082fe7595c1"
	)
	path := writeUsageDB(t,
		// Same account, same provider, two different limit ids.
		usageRow{RecordedAtMs: 10, Provider: "anthropic", AccountKey: oauthKey, LimitID: "anthropic:5h", Label: "5h", UsedFraction: 0.11},
		usageRow{RecordedAtMs: 20, Provider: "anthropic", AccountKey: oauthKey, LimitID: "anthropic:7d", Label: "7d", UsedFraction: 0.22},
		// Same provider and limit id, a different account_key.
		usageRow{RecordedAtMs: 30, Provider: "anthropic", AccountKey: secretKey, LimitID: "anthropic:5h", Label: "5h", UsedFraction: 0.33},
		// A different provider entirely, and one with no email at all.
		usageRow{RecordedAtMs: 40, Provider: "xai-oauth", AccountKey: apiKey, LimitID: "xai-oauth:credits:1w", Label: "credits", UsedFraction: 0.44},
		usageRow{RecordedAtMs: 50, Provider: "openai-codex", AccountKey: oauthKey, LimitID: "openai-codex:primary", Label: "5h", UsedFraction: 0.55},
		// A superseded row inside one of the groups above.
		usageRow{RecordedAtMs: 5, Provider: "openai-codex", AccountKey: oauthKey, LimitID: "openai-codex:primary", Label: "5h", UsedFraction: 0.99},
	)

	type key struct{ provider, account, limitID string }
	want := map[key]float64{
		{"anthropic", oauthKey, "anthropic:5h"}:            0.11,
		{"anthropic", oauthKey, "anthropic:7d"}:            0.22,
		{"anthropic", secretKey, "anthropic:5h"}:           0.33,
		{"xai-oauth", apiKey, "xai-oauth:credits:1w"}:      0.44,
		{"openai-codex", oauthKey, "openai-codex:primary"}: 0.55,
	}

	got := LatestUsageWindows(path)
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got %+v", len(got), len(want), got)
	}
	seen := make(map[key]bool, len(got))
	for _, w := range got {
		k := key{w.Provider, w.AccountKey, w.LimitID}
		fraction, ok := want[k]
		if !ok {
			t.Errorf("unexpected window %+v", k)
			continue
		}
		if seen[k] {
			t.Errorf("duplicate window for %+v", k)
		}
		seen[k] = true
		if !closeEnough(w.UsedFraction, fraction) {
			t.Errorf("%+v UsedFraction = %v, want %v", k, w.UsedFraction, fraction)
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("missing window %+v", k)
		}
	}
}

func TestLatestUsageWindowsNormalisesIDs(t *testing.T) {
	path := writeUsageDB(t, usageRow{
		RecordedAtMs: 1, Provider: "  ANTHROPIC  ", AccountKey: "  oauth|secret:c5f777162aca2d15  ",
		LimitID: "ANTHROPIC:5H", Label: "Current session", UsedFraction: 0.5,
	})

	got := LatestUsageWindows(path)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	w := got[0]
	if w.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", w.Provider, "anthropic")
	}
	if w.LimitID != "anthropic:5h" {
		t.Errorf("LimitID = %q, want %q", w.LimitID, "anthropic:5h")
	}
	// AccountKey is documented as opaque: it must survive verbatim, since
	// mangling it would break the join back to OMP's own records.
	if w.AccountKey != "  oauth|secret:c5f777162aca2d15  " {
		t.Errorf("AccountKey = %q, want it preserved verbatim", w.AccountKey)
	}
	if w.Label != "Current session" {
		t.Errorf("Label = %q, want it preserved verbatim", w.Label)
	}
}

func TestLatestUsageWindowsNullColumns(t *testing.T) {
	path := writeUsageDB(t, usageRow{
		RecordedAtMs: 1_700_000_000_000,
		Provider:     "opencode-go",
		AccountKey:   "api_key|secret:e9554082fe7595c1",
		LimitID:      "rolling-5h",
		Label:        "5h",
		// email, account_id, window_label, used_fraction, status and
		// resets_at are all left nil, i.e. inserted as SQL NULL.
	})

	got := LatestUsageWindows(path)
	if len(got) != 1 {
		t.Fatalf("len = %d, want the NULL-bearing row to survive; got %+v", len(got), got)
	}
	w := got[0]
	if w.Email != "" || w.AccountID != "" || w.WindowLabel != "" || w.Status != "" {
		t.Errorf("NULL text columns = (%q, %q, %q, %q), want empty strings",
			w.Email, w.AccountID, w.WindowLabel, w.Status)
	}
	if w.UsedFraction != 0 {
		t.Errorf("UsedFraction = %v, want 0", w.UsedFraction)
	}
	if w.ResetsAtMs != 0 {
		t.Errorf("ResetsAtMs = %d, want 0", w.ResetsAtMs)
	}
	if w.Provider != "opencode-go" || w.LimitID != "rolling-5h" {
		t.Errorf("identity = (%q, %q), want (opencode-go, rolling-5h)", w.Provider, w.LimitID)
	}
}

func TestLatestUsageWindowsFailuresReturnNil(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent", "agent.db")

	notSQLite := filepath.Join(t.TempDir(), "agent.db")
	if err := os.WriteFile(notSQLite, []byte("this is not a database\n"), 0o644); err != nil {
		t.Fatalf("write junk file: %v", err)
	}

	dir := t.TempDir()

	tests := []struct {
		name string
		path string
	}{
		{"empty path", ""},
		{"missing file", missing},
		{"not a sqlite file", notSQLite},
		{"directory instead of file", dir},
		{"sqlite without usage_history", sqliteWithoutUsageHistory(t)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := LatestUsageWindows(tc.path)
			if got != nil {
				t.Fatalf("LatestUsageWindows(%q) = %+v, want nil", tc.path, got)
			}
		})
	}
}

// sqliteWithoutUsageHistory builds a real SQLite file from an OMP old enough
// to predate the usage_history table.
func sqliteWithoutUsageHistory(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE auth_credentials (
		provider TEXT NOT NULL, credential_type TEXT, disabled_cause TEXT, updated_at INTEGER)`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	return path
}

func closeEnough(got, want float64) bool {
	return math.Abs(got-want) < 1e-9
}
