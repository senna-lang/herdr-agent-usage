/**
 * Cursor statusLine payload adapter.
 *
 * Cursor spawns a configured command on every conversation update and pipes it
 * a JSON session description on stdin. This translates that vendor payload into
 * a Snapshot and stops there: nothing downstream sees Cursor's field shapes.
 *
 * Context occupancy comes from context_window.total_input_tokens. Cursor
 * documents that field as derived upstream from used_percentage, so it is
 * quantised to one decimal place of the window (0.1%, i.e. ~256 tokens in a
 * 256k window). That precision is below what the $context row renders, which
 * rounds to whole percent. context_window.current_usage is deliberately not
 * used: it describes the last API call rather than the current window, is
 * absent before the first call, and only coincides with occupancy on a turn
 * that happened to read the entire prior context from cache.
 */
package cursor

import (
	"encoding/json"
	"errors"
	"strings"
)

// Errors reported when a payload cannot produce a usable snapshot. Each one
// must leave any existing snapshot untouched rather than replacing it.
var (
	ErrMalformedPayload = errors.New("cursor: malformed statusLine payload")
	ErrNoSessionID      = errors.New("cursor: statusLine payload has no session_id")
	ErrNoContextTokens  = errors.New("cursor: statusLine payload reports no context tokens")
)

// statusLinePayload is the subset of Cursor's statusLine JSON this adapter
// consumes. Pointer fields distinguish "reported as null" from "reported zero",
// which matters because Cursor nulls the usage fields early in a session.
type statusLinePayload struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Model     struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	ContextWindow struct {
		TotalInputTokens  *int `json:"total_input_tokens"`
		ContextWindowSize *int `json:"context_window_size"`
	} `json:"context_window"`
}

// SnapshotFromStatusLine converts one statusLine payload into a Snapshot.
//
// paneID is herdr's pane id for the pane Cursor is running in, recorded so the
// provider can still resolve this session after Cursor rotates its session id.
// It may be empty when the bridge runs outside herdr.
func SnapshotFromStatusLine(payload []byte, paneID string, nowMs int64) (Snapshot, error) {
	var parsed statusLinePayload
	if len(strings.TrimSpace(string(payload))) == 0 {
		return Snapshot{}, ErrMalformedPayload
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return Snapshot{}, ErrMalformedPayload
	}
	if parsed.SessionID == "" {
		return Snapshot{}, ErrNoSessionID
	}
	if parsed.ContextWindow.TotalInputTokens == nil {
		return Snapshot{}, ErrNoContextTokens
	}

	snap := Snapshot{
		SessionID:     parsed.SessionID,
		PaneID:        paneID,
		Model:         modelLabel(parsed),
		Cwd:           workingDir(parsed),
		ContextTokens: *parsed.ContextWindow.TotalInputTokens,
		UpdatedAtMs:   nowMs,
	}
	// A zero or absent window size is not a window: reporting it as one would
	// render a meaningless 0% occupancy instead of the bare token count.
	if size := parsed.ContextWindow.ContextWindowSize; size != nil && *size > 0 {
		window := *size
		snap.WindowTokens = &window
	}
	return snap, nil
}

func modelLabel(parsed statusLinePayload) string {
	if parsed.Model.DisplayName != "" {
		return parsed.Model.DisplayName
	}
	return parsed.Model.ID
}

// workingDir prefers the top-level cwd, which Cursor documents as carrying the
// same value as workspace.current_dir; the nested field is the fallback.
func workingDir(parsed statusLinePayload) string {
	if parsed.Cwd != "" {
		return parsed.Cwd
	}
	return parsed.Workspace.CurrentDir
}
