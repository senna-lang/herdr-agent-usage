package omp

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
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

func TestOMPProvider_RehomesStaleSessionPath(t *testing.T) {
	root := t.TempDir()
	cwd := "/Users/senna/Documents/Repos/life"
	filename := "2026-08-10T01-06-25-252Z_019fe934-e364-7000-a8e5-6c59127e8a7a.jsonl"
	dir := filepath.Join(root, EncodeOMPSessionDir(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(`{"type":"message","message":{"role":"assistant","provider":"openai","model":"gpt-5","contextSnapshot":{"promptTokens":59000}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMP_SESSIONS_ROOT", root)

	usage := Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{
			Kind:  "path",
			Value: filepath.Join(root, "home-life-legacy", filename),
		},
		Cwd: &cwd,
	})
	if usage == nil || usage.ContextTokens != 59000 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestOMPProvider_DoesNotUseLatestSessionForIDOnlyInput(t *testing.T) {
	root := t.TempDir()
	cwd := "/Users/senna/Documents/Repos/life"
	dir := filepath.Join(root, EncodeOMPSessionDir(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-08-11T01-00-00Z_other-pane.jsonl"), []byte(`{"type":"message","message":{"role":"assistant","provider":"openai","model":"gpt-5","contextSnapshot":{"promptTokens":59000}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMP_SESSIONS_ROOT", root)

	usage := Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: "missing-session-id"},
		Cwd:     &cwd,
	})
	if usage != nil {
		t.Fatalf("usage = %+v; ID-only session must not inherit another pane's transcript", usage)
	}
}
