/**
 * Tests for ToContextUsage.
 */
package opencode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToContextUsage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	if err := os.WriteFile(path, []byte(`{
		"opencode-go": { "models": { "minimax-m3": { "limit": { "context": 1000000 } } } }
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_MODELS_PATH", path)
	ClearModelsCatalogCache()
	t.Cleanup(ClearModelsCatalogCache)

	mid, pid := "minimax-m3", "opencode-go"
	got := ToContextUsage(MessageUsage{ContextTokens: 16_709, ModelID: &mid, ProviderID: &pid})
	if got.ContextTokens != 16_709 || got.WindowTokens == nil || *got.WindowTokens != 1_000_000 {
		t.Fatalf("got %+v", got)
	}

	unk := "unknown"
	got = ToContextUsage(MessageUsage{ContextTokens: 1000, ModelID: &unk, ProviderID: &pid})
	if got.ContextTokens != 1000 || got.WindowTokens != nil {
		t.Fatalf("unknown got %+v", got)
	}
}
