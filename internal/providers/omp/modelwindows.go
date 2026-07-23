/**
 * Resolves context window sizes from OMP's local models.db cache.
 */
package omp

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type modelWindowCache struct {
	mu      sync.Mutex
	loaded  time.Time
	path    string
	windows map[string]int // "provider/model" and bare "model"
}

var globalModelWindows modelWindowCache

func modelsDBPath() string {
	if v := os.Getenv("OMP_MODELS_DB"); v != "" {
		return v
	}
	if v := os.Getenv("PI_MODELS_DB"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, rel := range []string{
		filepath.Join(".pi", "agent", "models.db"),
		filepath.Join(".omp", "agent", "models.db"),
	} {
		path := filepath.Join(home, rel)
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	return filepath.Join(home, ".omp", "agent", "models.db")
}

type catalogModel struct {
	ID            string `json:"id"`
	ContextWindow int    `json:"contextWindow"`
}

func loadWindowsFromDB(path string) map[string]int {
	out := map[string]int{}
	if path == "" {
		return out
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return out
	}
	defer db.Close()
	rows, err := db.Query(`SELECT provider_id, models FROM model_cache`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var providerID, modelsJSON string
		if err := rows.Scan(&providerID, &modelsJSON); err != nil {
			continue
		}
		var models []catalogModel
		if err := json.Unmarshal([]byte(modelsJSON), &models); err != nil {
			continue
		}
		for _, model := range models {
			if model.ID == "" || model.ContextWindow <= 0 {
				continue
			}
			out[model.ID] = model.ContextWindow
			if providerID != "" {
				out[providerID+"/"+model.ID] = model.ContextWindow
			}
		}
	}
	return out
}

func windowsForLookup() map[string]int {
	path := modelsDBPath()
	globalModelWindows.mu.Lock()
	defer globalModelWindows.mu.Unlock()
	if path != "" && (globalModelWindows.windows == nil || globalModelWindows.path != path || time.Since(globalModelWindows.loaded) > 60*time.Second) {
		globalModelWindows.path = path
		globalModelWindows.windows = loadWindowsFromDB(path)
		globalModelWindows.loaded = time.Now()
	}
	if globalModelWindows.windows == nil {
		return map[string]int{}
	}
	return globalModelWindows.windows
}

// ContextWindowFor returns the context window for provider/model when known.
func ContextWindowFor(provider, model string) *int {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	windows := windowsForLookup()
	if provider != "" {
		if n, ok := windows[provider+"/"+model]; ok {
			v := n
			return &v
		}
	}
	if n, ok := windows[model]; ok {
		v := n
		return &v
	}
	return nil
}
