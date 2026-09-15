/**
 * Antigravity statusLine command behaviour.
 *
 * Antigravity spawns this on every status update, so it must be cheap, must
 * never leave a partial snapshot behind, and must not overwrite a good
 * snapshot with an unusable payload. Configuring it fully replaces
 * Antigravity's built-in statusLine text (see `/statusline help`), so the
 * rendered line stands alone rather than chaining with a default Antigravity
 * renders elsewhere.
 */
package antigravity

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// RunStatusLineIn records one statusLine payload and returns the line to
// render.
//
// A payload that cannot produce a usable snapshot is reported as an error and
// leaves stored state untouched: an empty, partial, or malformed update must
// never displace the last good observation.
func RunStatusLineIn(sessionsDir string, payload []byte, paneID string, nowMs int64) (string, error) {
	snap, err := SnapshotFromStatusLine(payload, paneID, nowMs)
	if err != nil {
		return "", err
	}
	if err := WriteSnapshot(sessionsDir, snap); err != nil {
		return "", err
	}
	PruneStale(sessionsDir, nowMs, SnapshotFreshnessMs)
	return statusLineText(snap), nil
}

// statusLineText renders through the shared context formatter, so
// Antigravity's own status line and the herdr $context row describe usage
// identically. No column budget is applied: this line is not constrained by
// the sidebar's width.
func statusLineText(snap Snapshot) string {
	usage := toContextUsage(snap)
	text := core.FormatUsageStatus(*usage, core.FormatUsageOptions{})
	if snap.Model == "" {
		return text
	}
	return snap.Model + "  " + text
}
