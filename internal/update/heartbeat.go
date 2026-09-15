/**
 * Tracks whether the Agent Usage pane is currently collecting so the idle
 * watcher can back off instead of running a second clock.
 *
 * A bare timestamp is not enough: if the Agent Usage pane process keeps
 * running across a `go build` without being restarted, it keeps ticking a
 * fresh-looking heartbeat using whatever publish logic it was compiled
 * with — even if that build predates the $cache_* and $limit publish calls entirely.
 * The watcher would then see "recently collected" forever and permanently
 * skip its own periodic publish, leaving sidebar tokens frozen. Tagging the
 * heartbeat with the writer's own build fingerprint lets a reader on a
 * different build refuse to trust it, timestamp notwithstanding.
 */
package update

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

var (
	buildFingerprintOnce sync.Once
	buildFingerprintVal  string
)

// buildFingerprint identifies the executable image this process is
// actually running. It is captured once, on first use, and never
// re-derived: os.Executable's path can resolve to a file a later `go
// build` has since replaced, which would silently launder a stale
// process into looking like current code again.
func buildFingerprint() string {
	buildFingerprintOnce.Do(func() {
		path, err := os.Executable()
		if err != nil {
			buildFingerprintVal = "unknown"
			return
		}
		info, err := os.Stat(path)
		if err != nil {
			buildFingerprintVal = "unknown"
			return
		}
		buildFingerprintVal = fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size())
	})
	return buildFingerprintVal
}

// TouchPaneHeartbeat records that the Agent Usage pane just collected,
// tagged with this process's build fingerprint.
func TouchPaneHeartbeat(now time.Time) {
	touchPaneHeartbeatWith(paneHeartbeatPath(), now, buildFingerprint())
}

func touchPaneHeartbeatWith(path string, now time.Time, fingerprint string) {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	line := strconv.FormatInt(now.UnixMilli(), 10) + "|" + fingerprint + "\n"
	_ = os.WriteFile(path, []byte(line), 0o644)
}

// PaneHeartbeatFresh reports whether the Agent Usage pane collected
// recently enough, on this same build, that the idle watcher should skip
// this tick. A heartbeat stamped with a different build fingerprint is
// never fresh — including one with no fingerprint at all, from a pre-fix
// binary — because the writer may lack the current publish logic even
// though it is still alive and ticking.
func PaneHeartbeatFresh(now time.Time, freshFor time.Duration) bool {
	return paneHeartbeatFreshWith(paneHeartbeatPath(), now, freshFor, buildFingerprint())
}

func paneHeartbeatFreshWith(path string, now time.Time, freshFor time.Duration, myFingerprint string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	msPart, fingerprint, ok := strings.Cut(trimNewline(string(raw)), "|")
	if !ok || fingerprint != myFingerprint {
		return false
	}
	ms, err := strconv.ParseInt(msPart, 10, 64)
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
