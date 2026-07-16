/**
 * Resolves a model's context limit from the OpenCode models cache.
 * Cache: $XDG_CACHE_HOME/opencode/models.json or ~/.cache/opencode/models.json
 * Override: OPENCODE_MODELS_PATH
 *
 * Structure: { [providerID]: { models: { [modelID]: { limit: { context } } } } }
 */
package opencode

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
)

type modelLimit struct {
	Context *float64 `json:"context"`
}

type modelEntry struct {
	Limit *modelLimit `json:"limit"`
}

type providerEntry struct {
	Models map[string]modelEntry `json:"models"`
}

type modelsCatalog map[string]providerEntry

var (
	catalogMu sync.Mutex
	cached    *struct {
		path    string
		mtimeMs int64
		catalog modelsCatalog
	}
)

// ResolveModelsJSONPath returns the models.json path if present.
func ResolveModelsJSONPath() string {
	if explicit := os.Getenv("OPENCODE_MODELS_PATH"); explicit != "" {
		if st, err := os.Stat(explicit); err == nil && st.Mode().IsRegular() {
			return explicit
		}
		return ""
	}
	base := ""
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		base = filepath.Join(xdg, "opencode")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache", "opencode")
	}
	path := filepath.Join(base, "models.json")
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
		return path
	}
	return ""
}

func loadCatalog() modelsCatalog {
	path := ResolveModelsJSONPath()
	if path == "" {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	mtimeMs := st.ModTime().UnixMilli()

	catalogMu.Lock()
	defer catalogMu.Unlock()
	if cached != nil && cached.path == path && cached.mtimeMs == mtimeMs {
		return cached.catalog
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var catalog modelsCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil
	}
	cached = &struct {
		path    string
		mtimeMs int64
		catalog modelsCatalog
	}{path: path, mtimeMs: mtimeMs, catalog: catalog}
	return catalog
}

// ClearModelsCatalogCache drops the cache (for tests).
func ClearModelsCatalogCache() {
	catalogMu.Lock()
	cached = nil
	catalogMu.Unlock()
}

// ContextWindowFor returns limit.context for a providerID + modelID pair.
func ContextWindowFor(providerID, modelID *string) *int {
	if providerID == nil || modelID == nil || *providerID == "" || *modelID == "" {
		return nil
	}
	catalog := loadCatalog()
	if catalog == nil {
		return nil
	}
	provider, ok := catalog[*providerID]
	if !ok || provider.Models == nil {
		return nil
	}
	model, ok := provider.Models[*modelID]
	if !ok || model.Limit == nil || model.Limit.Context == nil {
		return nil
	}
	ctx := *model.Limit.Context
	if !isFinite(ctx) || ctx <= 0 {
		return nil
	}
	v := int(ctx)
	return &v
}

func isFinite(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0)
}
