/**
 * Resolves a model's context limit from Kilo's model catalog.
 * Cache: $XDG_CACHE_HOME/kilo/models.json or ~/.cache/kilo/models.json
 * Override: KILO_MODELS_PATH
 *
 * Structure: { [providerID]: { models: { [modelID]: { limit: { context } } } } }
 *
 * This is the same catalog shape the OpenCode provider reads, from Kilo's own
 * cache directory. The file is several megabytes and Kilo rewrites it when it
 * refreshes its provider list, so it is parsed at most once per (path, mtime)
 * rather than once per pane.
 */
package kilo

import (
	"encoding/json"
	"math"
	"os"
	"sync"
)

type modelLimit struct {
	Context *float64 `json:"context"`
}

type modelEntry struct {
	Limit *modelLimit `json:"limit"`
}

type providerEntry struct {
	Name   string                `json:"name"`
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

// ClearModelsCatalogCache drops the catalog cache (for tests).
func ClearModelsCatalogCache() {
	catalogMu.Lock()
	cached = nil
	catalogMu.Unlock()
}

func loadCatalog() modelsCatalog {
	path := ResolveKiloModelsPath()
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

// ProviderDisplayName returns the catalog's display name for a provider
// ("opencode-go" -> "OpenCode Go"). Empty when the provider is absent, letting
// callers pick their own fallback.
func ProviderDisplayName(providerID string) string {
	if providerID == "" {
		return ""
	}
	catalog := loadCatalog()
	if catalog == nil {
		return ""
	}
	return catalog[providerID].Name
}

// ContextWindowFor returns limit.context for a providerID + modelID pair.
//
// A missing entry is normal: Kilo's catalog covers the providers it has been
// shown, and a session can name a model that was since dropped from it. That
// resolves to no window, which renders as a bare token count rather than a
// percentage against an unknown denominator.
func ContextWindowFor(providerID, modelID string) *int {
	if providerID == "" || modelID == "" {
		return nil
	}
	catalog := loadCatalog()
	if catalog == nil {
		return nil
	}
	provider, ok := catalog[providerID]
	if !ok || provider.Models == nil {
		return nil
	}
	model, ok := provider.Models[modelID]
	if !ok || model.Limit == nil || model.Limit.Context == nil {
		return nil
	}
	ctx := *model.Limit.Context
	if math.IsNaN(ctx) || math.IsInf(ctx, 0) || ctx <= 0 {
		return nil
	}
	v := int(ctx)
	return &v
}
