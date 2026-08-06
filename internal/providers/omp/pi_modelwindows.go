/**
 * Resolves context windows for the stock Pi coding agent.
 *
 * Pi persists refreshed provider catalogs in models-store.json and user model
 * definitions/overrides in models.json. Both live in PI_CODING_AGENT_DIR
 * (normally ~/.pi/agent). The Herdr integration reports the session path, so
 * we can also infer the matching agent directory when Pi uses a non-default
 * config directory.
 */
package omp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const piDefaultContextWindow = 128_000

type piCatalogModel struct {
	ID            string `json:"id"`
	ContextWindow *int   `json:"contextWindow"`
}

type piStoredCatalog struct {
	Models []piCatalogModel `json:"models"`
}

type piModelOverride struct {
	ContextWindow *int `json:"contextWindow"`
}

type piConfiguredProvider struct {
	Models         []piCatalogModel           `json:"models"`
	ModelOverrides map[string]piModelOverride `json:"modelOverrides"`
}

type piModelsConfig struct {
	Providers map[string]piConfiguredProvider `json:"providers"`
}

func addUniquePath(paths []string, seen map[string]bool, path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return paths
	}
	path = filepath.Clean(expandHome(path))
	if seen[path] {
		return paths
	}
	seen[path] = true
	return append(paths, path)
}

// piAgentDirs returns candidate config directories in priority order. A
// session under <agent-dir>/sessions/<encoded-cwd>/<file>.jsonl reveals its
// own agent dir, which keeps custom PI_CODING_AGENT_DIR installations working
// even though Herdr's plugin process does not inherit the pane environment.
func piAgentDirs(sessionPath string) []string {
	var dirs []string
	seen := map[string]bool{}
	dirs = addUniquePath(dirs, seen, os.Getenv("PI_CODING_AGENT_DIR"))

	path := filepath.Clean(expandHome(sessionPath))
	if strings.HasSuffix(path, ".jsonl") {
		projectDir := filepath.Dir(path)
		sessionsDir := filepath.Dir(projectDir)
		if filepath.Base(sessionsDir) == "sessions" {
			dirs = addUniquePath(dirs, seen, filepath.Dir(sessionsDir))
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		dirs = addUniquePath(dirs, seen, filepath.Join(home, ".pi", "agent"))
	}
	return dirs
}

func setPiWindow(windows map[string]int, provider, model string, window int) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if model == "" || window <= 0 {
		return
	}
	windows[model] = window
	if provider != "" {
		windows[provider+"/"+model] = window
	}
}

func loadPiStoredWindows(path string, windows map[string]int) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var catalogs map[string]piStoredCatalog
	if err := json.Unmarshal(raw, &catalogs); err != nil {
		return
	}
	for provider, catalog := range catalogs {
		for _, model := range catalog.Models {
			if model.ContextWindow != nil {
				setPiWindow(windows, provider, model.ID, *model.ContextWindow)
			}
		}
	}
}

// loadPiConfiguredWindows applies models.json after models-store.json, just as
// Pi applies user models and modelOverrides on top of its built-in catalogs.
func loadPiConfiguredWindows(path string, windows map[string]int) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var config piModelsConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return
	}
	for provider, definition := range config.Providers {
		for _, model := range definition.Models {
			window := piDefaultContextWindow
			if model.ContextWindow != nil {
				window = *model.ContextWindow
			}
			setPiWindow(windows, provider, model.ID, window)
		}
		for model, override := range definition.ModelOverrides {
			if override.ContextWindow != nil {
				setPiWindow(windows, provider, model, *override.ContextWindow)
			}
		}
	}
}

func loadPiWindows(sessionPath string) map[string]int {
	windows := map[string]int{}
	// Lower-priority defaults are loaded first so an inferred custom agent dir
	// or explicit PI_CODING_AGENT_DIR can override them below.
	dirs := piAgentDirs(sessionPath)
	for i := len(dirs) - 1; i >= 0; i-- {
		loadPiStoredWindows(filepath.Join(dirs[i], "models-store.json"), windows)
		loadPiConfiguredWindows(filepath.Join(dirs[i], "models.json"), windows)
	}
	return windows
}

// PiContextWindowFor returns Pi's configured context window for the model.
// Exact provider/model matches win; a bare-model fallback supports older
// catalog rows that omitted provider metadata.
func PiContextWindowFor(sessionPath, provider, model string) *int {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	windows := loadPiWindows(sessionPath)
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
