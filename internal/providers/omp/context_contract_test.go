package omp

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// OMP and Pi share this adapter. The sidebar percentage must use the latest
// assistant turn's model, not an earlier model from the same session.
func TestSidebarContextContract_UsesLatestModelWindow(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "models.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE model_cache (provider_id TEXT, models TEXT)`); err != nil {
		t.Fatal(err)
	}
	models := `[
		{"id":"small-model","contextWindow":200000},
		{"id":"large-model","contextWindow":1000000}
	]`
	if _, err := db.Exec(`INSERT INTO model_cache(provider_id, models) VALUES (?, ?)`, "deepseek", models); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	sessionPath := filepath.Join(dir, "session.jsonl")
	lines := []string{
		`{"type":"message","message":{"role":"assistant","provider":"deepseek","model":"small-model","contextSnapshot":{"promptTokens":100000},"usage":{"totalTokens":100000}}}`,
		`{"type":"message","message":{"role":"assistant","provider":"deepseek","model":"large-model","contextSnapshot":{"promptTokens":250000},"usage":{"totalTokens":250000}}}`,
	}
	if err := os.WriteFile(sessionPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OMP_MODELS_DB", dbPath)
	globalModelWindows = modelWindowCache{}
	t.Cleanup(func() { globalModelWindows = modelWindowCache{} })

	usage := ResolveUsageForPath(sessionPath)
	if usage == nil || usage.ContextTokens != 250_000 || usage.WindowTokens == nil || *usage.WindowTokens != 1_000_000 {
		t.Fatalf("latest model usage = %+v", usage)
	}
	status := core.FormatUsageStatus(*usage, core.FormatUsageOptions{})
	if !strings.Contains(status, "25%") || !strings.Contains(status, "250k") {
		t.Fatalf("status %q does not reflect 250k / 1M", status)
	}
}
