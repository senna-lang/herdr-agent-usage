/**
 * Tests for OpenCode ResolveUsage (uses a temporary SQLite database).
 */
package opencode

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func setupOpenCodeDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "opencode.db")
	t.Setenv("OPENCODE_DB", dbPath)
	ClearModelsCatalogCache()
	t.Cleanup(ClearModelsCatalogCache)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE project (
			id TEXT PRIMARY KEY,
			worktree TEXT NOT NULL,
			sandboxes TEXT NOT NULL,
			time_created INTEGER NOT NULL,
			time_updated INTEGER NOT NULL
		);
		CREATE TABLE session (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			slug TEXT NOT NULL,
			directory TEXT NOT NULL,
			title TEXT NOT NULL,
			version TEXT NOT NULL,
			time_created INTEGER NOT NULL,
			time_updated INTEGER NOT NULL,
			time_archived INTEGER,
			cost REAL DEFAULT 0 NOT NULL,
			tokens_input INTEGER DEFAULT 0 NOT NULL,
			tokens_output INTEGER DEFAULT 0 NOT NULL,
			tokens_reasoning INTEGER DEFAULT 0 NOT NULL,
			tokens_cache_read INTEGER DEFAULT 0 NOT NULL,
			tokens_cache_write INTEGER DEFAULT 0 NOT NULL
		);
		CREATE TABLE message (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			time_created INTEGER NOT NULL,
			time_updated INTEGER NOT NULL,
			data TEXT NOT NULL
		);
		INSERT INTO project VALUES ('p1', '/proj', '[]', 1, 1);
		INSERT INTO session VALUES (
			'ses_abc', 'p1', 'slug', '/proj/app', 'title', '1',
			1, 100, NULL, 0, 0, 0, 0, 0, 0
		);
		INSERT INTO message VALUES (
			'm1', 'ses_abc', 1, 50,
			'{"role":"user"}'
		);
		INSERT INTO message VALUES (
			'm2', 'ses_abc', 2, 100,
			'{"role":"assistant","modelID":"minimax-m3","providerID":"opencode-go","tokens":{"input":100,"output":20,"cache":{"read":400,"write":0}}}'
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	modelsPath := filepath.Join(dir, "models.json")
	if err := os.WriteFile(modelsPath, []byte(`{
		"opencode-go": { "models": { "minimax-m3": { "limit": { "context": 1000000 } } } }
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_MODELS_PATH", modelsPath)
}

func TestResolveUsageForOpenCode_BySession(t *testing.T) {
	setupOpenCodeDB(t)
	sid := "ses_abc"
	usage := ResolveUsageForOpenCode(&sid, nil)
	if usage == nil || usage.ContextTokens != 500 || usage.WindowTokens == nil || *usage.WindowTokens != 1_000_000 {
		t.Fatalf("got %+v", usage)
	}
}

func TestResolveUsageForOpenCode_ByCwd(t *testing.T) {
	setupOpenCodeDB(t)
	cwd := "/proj/app"
	usage := ResolveUsageForOpenCode(nil, &cwd)
	if usage == nil || usage.ContextTokens != 500 || usage.WindowTokens == nil || *usage.WindowTokens != 1_000_000 {
		t.Fatalf("got %+v", usage)
	}
}

func TestResolveUsageForOpenCode_Unknown(t *testing.T) {
	setupOpenCodeDB(t)
	sid := "ses_missing"
	if ResolveUsageForOpenCode(&sid, nil) != nil {
		t.Fatal("expected nil")
	}
}
