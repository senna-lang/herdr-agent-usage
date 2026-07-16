/**
 * Tests for ContextWindowFor / models.json resolution.
 */
package opencode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContextWindowFor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	if err := os.WriteFile(path, []byte(`{
		"opencode-go": {
			"models": {
				"minimax-m3": { "limit": { "context": 1000000, "output": 131072 } }
			}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_MODELS_PATH", path)
	ClearModelsCatalogCache()
	t.Cleanup(ClearModelsCatalogCache)

	p, m := "opencode-go", "minimax-m3"
	got := ContextWindowFor(&p, &m)
	if got == nil || *got != 1_000_000 {
		t.Fatalf("got %#v", got)
	}
	nope := "nope"
	if ContextWindowFor(&p, &nope) != nil {
		t.Fatal("expected nil for unknown model")
	}
	if ContextWindowFor(nil, &m) != nil {
		t.Fatal("expected nil for nil provider")
	}
	if ResolveModelsJSONPath() != path {
		t.Fatalf("path = %q", ResolveModelsJSONPath())
	}
}
