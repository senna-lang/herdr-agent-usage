/**
 * Tracks whether the Agent Usage pane is currently collecting so the idle
 * watcher can back off instead of running a second clock.
 */
package update

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	// PaneRefreshInterval is the Agent Usage pane's collect tick.
	PaneRefreshInterval = 15 * time.Second
	// WatchRefreshInterval is the idle $limit collect tick while the pane is closed.
	WatchRefreshInterval = 60 * time.Second
	heartbeatFileName    = "limits-pane.heartbeat"
	// heartbeatFreshFor is slightly longer than PaneRefreshInterval so a
	// slow collect cannot look like the pane closed.
	heartbeatFreshFor = 20 * time.Second
)

func pluginStateDir() string {
	if v := os.Getenv("USAGEBAR_STATE_DIR"); v != "" {
		_ = os.MkdirAll(v, 0o755)
		return v
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".claude", "herdr-usagebar")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func paneHeartbeatPath() string {
	if v := os.Getenv("USAGEBAR_PANE_HEARTBEAT_PATH"); v != "" {
		return v
	}
	return filepath.Join(pluginStateDir(), heartbeatFileName)
}

// TouchPaneHeartbeat records that the Agent Usage pane just collected.
func TouchPaneHeartbeat(now time.Time) {
	path := paneHeartbeatPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(strconv.FormatInt(now.UnixMilli(), 10)+"\n"), 0o644)
}

// PaneHeartbeatFresh reports whether the Agent Usage pane collected recently
// enough that the idle watcher should skip this tick.
func PaneHeartbeatFresh(now time.Time, freshFor time.Duration) bool {
	raw, err := os.ReadFile(paneHeartbeatPath())
	if err != nil {
		return false
	}
	ms, err := strconv.ParseInt(trimNewline(string(raw)), 10, 64)
	if err != nil || ms <= 0 {
		return false
	}
	written := time.UnixMilli(ms)
	return now.Sub(written) >= 0 && now.Sub(written) <= freshFor
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
