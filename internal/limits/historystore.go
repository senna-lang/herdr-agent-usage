/**
 * Persists usage-history samples so recent-pace slopes survive pane restarts.
 */
package limits

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func historyBaseDir() string {
	dir := filepath.Join(userHome(), ".claude", "herdr-usagebar")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func userHome() string {
	h, _ := os.UserHomeDir()
	return h
}

func historyFilePath() string {
	if v := os.Getenv("USAGEBAR_HISTORY_PATH"); v != "" {
		return v
	}
	return filepath.Join(historyBaseDir(), "usage-history.json")
}

// LoadUsageHistory loads persisted samples (or empty).
func LoadUsageHistory() UsageHistory {
	raw, err := os.ReadFile(historyFilePath())
	if err != nil {
		return UsageHistory{}
	}
	var h UsageHistory
	if err := json.Unmarshal(raw, &h); err != nil || h == nil {
		return UsageHistory{}
	}
	return h
}

// SaveUsageHistory atomically writes history (best-effort).
func SaveUsageHistory(history UsageHistory) {
	path := historyFilePath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	b, err := json.Marshal(history)
	if err != nil {
		return
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
