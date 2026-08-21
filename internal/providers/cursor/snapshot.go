/**
 * Cursor context snapshots.
 *
 * Cursor records no context or token usage on disk; the CLI statusLine is the
 * only local surface that reports it, and it reports by push rather than by
 * file. A snapshot is this plugin's own record of one statusLine invocation,
 * written by the bridge and read back by the provider.
 *
 * Writes are atomic (temp file in the same directory, then rename) because the
 * statusLine contract kills an in-flight command whenever a newer update
 * arrives or the timeout elapses. A truncating write would let that kill leave
 * a partial file in place of the last good snapshot.
 */
package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Snapshot is one statusLine observation of a Cursor session's context.
type Snapshot struct {
	SessionID     string `json:"session_id"`
	PaneID        string `json:"pane_id,omitempty"`
	Model         string `json:"model,omitempty"`
	Cwd           string `json:"cwd,omitempty"`
	ContextTokens int    `json:"context_tokens"`
	// WindowTokens is the active context window size. Nil when Cursor did not
	// report one, in which case only the absolute token count is displayable.
	WindowTokens *int `json:"window_tokens,omitempty"`
	// UpdatedAtMs is the write time in Unix milliseconds. Milliseconds rather
	// than seconds so two snapshots written in quick succession still order
	// deterministically, which pane-identity resolution depends on.
	UpdatedAtMs int64 `json:"updated_at_ms"`
}

// snapshotPath is the canonical path for one session's snapshot.
func snapshotPath(sessionsDir, sessionID string) string {
	return filepath.Join(sessionsDir, sessionID+".json")
}

// WriteSnapshot atomically stores snap under sessionsDir.
//
// The caller must have validated snap: this function will happily persist
// whatever it is given, and an invalid snapshot written here would replace a
// valid one. Validation lives in the statusLine adapter, before this point.
func WriteSnapshot(sessionsDir string, snap Snapshot) error {
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		return err
	}
	encoded, err := json.Marshal(snap)
	if err != nil {
		return err
	}

	// Same directory as the destination so the rename cannot cross filesystems.
	temp, err := os.CreateTemp(sessionsDir, ".snapshot-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		// No-op once the rename succeeded; removes the temp file on any
		// failure path so a crash mid-write leaves no debris behind.
		_ = os.Remove(tempName)
	}()

	if _, err := temp.Write(encoded); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, snapshotPath(sessionsDir, snap.SessionID))
}

// ReadSnapshot loads one session's snapshot. A missing or unreadable file and
// a malformed one are both reported as an error: neither is usable, and the
// caller must not distinguish "absent" from "corrupt" when deciding to display.
func ReadSnapshot(sessionsDir, sessionID string) (Snapshot, error) {
	return readSnapshotFile(snapshotPath(sessionsDir, sessionID))
}

func readSnapshotFile(path string) (Snapshot, error) {
	var snap Snapshot
	raw, err := os.ReadFile(path)
	if err != nil {
		return snap, err
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return snap, err
	}
	return snap, nil
}

// ListSnapshots returns every readable snapshot under sessionsDir. Unreadable
// and malformed entries are skipped rather than failing the whole listing: one
// corrupt file must not hide every other pane's usage.
func ListSnapshots(sessionsDir string) []Snapshot {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil
	}
	var out []Snapshot
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		snap, err := readSnapshotFile(filepath.Join(sessionsDir, entry.Name()))
		if err != nil {
			continue
		}
		out = append(out, snap)
	}
	return out
}

// RemoveSnapshot deletes one session's snapshot, used by session teardown.
func RemoveSnapshot(sessionsDir, sessionID string) error {
	err := os.Remove(snapshotPath(sessionsDir, sessionID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// PruneStale removes snapshots that can no longer be resolved, keeping the
// sessions directory bounded across a long-lived install. Cursor mints a new
// session id whenever a conversation is cleared, so without this the directory
// would accumulate one file per cleared conversation forever.
func PruneStale(sessionsDir string, nowMs, freshnessMs int64) {
	for _, snap := range ListSnapshots(sessionsDir) {
		if nowMs-snap.UpdatedAtMs > freshnessMs {
			_ = RemoveSnapshot(sessionsDir, snap.SessionID)
		}
	}
}
