/**
 * Cursor statusLine command behaviour.
 *
 * Cursor spawns this on every conversation update, so it must be cheap, must
 * never leave a partial snapshot behind, and must not overwrite a good snapshot
 * with an unusable payload. It returns the text Cursor renders above its prompt;
 * an error means "no update", which Cursor honours by keeping the previous text.
 */
package cursor

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// RunStatusLineIn records one statusLine payload and returns the line to render.
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

// statusLineText renders through the shared context formatter, so Cursor's own
// status line and the herdr $context row describe usage identically. No column
// budget is applied: this line is not constrained by the sidebar's width.
func statusLineText(snap Snapshot) string {
	usage := toContextUsage(snap)
	text := core.FormatUsageStatus(*usage, core.FormatUsageOptions{})
	if snap.Model == "" {
		return text
	}
	return snap.Model + "  " + text
}
