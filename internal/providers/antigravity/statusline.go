/**
 * Antigravity CLI statusLine payload adapter.
 *
 * Antigravity spawns a configured `/statusline <command>` on every status
 * update and pipes it a JSON session description on stdin, the same contract
 * Cursor's statusLine uses. This translates that vendor payload into a
 * Snapshot and stops there: nothing downstream sees Antigravity's field
 * shapes.
 *
 * Context occupancy comes from context_window.total_input_tokens, reported as
 * a plain (non-nullable) integer that is legitimately 0 before the first
 * turn — unlike Cursor, Antigravity never nulls this field out, so a captured
 * zero is a real "no usage yet" observation rather than a missing one.
 * context_window.current_usage, present only after the first turn, describes
 * the latest completed turn's cache breakdown and feeds Cache; it is not used
 * for ContextTokens, which the payload already reports as a running total.
 */
package antigravity

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// Errors reported when a payload cannot produce a usable snapshot. Each one
// must leave any existing snapshot untouched rather than replacing it.
var (
	ErrMalformedPayload = errors.New("antigravity: malformed statusLine payload")
	ErrNoSessionID      = errors.New("antigravity: statusLine payload has no conversation_id")
	ErrNoContextWindow  = errors.New("antigravity: statusLine payload has no context_window")
)

// statusLinePayload is the subset of Antigravity's statusLine JSON this
// adapter consumes.
type statusLinePayload struct {
	ConversationID string `json:"conversation_id"`
	Cwd            string `json:"cwd"`
	Model          struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	ContextWindow *struct {
		TotalInputTokens  int  `json:"total_input_tokens"`
		ContextWindowSize *int `json:"context_window_size"`
		CurrentUsage      *struct {
			InputTokens              int `json:"input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"current_usage"`
	} `json:"context_window"`
	Quota map[string]struct {
		RemainingFraction float64 `json:"remaining_fraction"`
		ResetTime         string  `json:"reset_time"`
	} `json:"quota"`
	PlanTier string `json:"plan_tier"`
	Email    string `json:"email"`
}

// SnapshotFromStatusLine converts one statusLine payload into a Snapshot.
//
// paneID is herdr's pane id for the pane Antigravity is running in, recorded
// so the provider can still resolve this session after Antigravity rotates
// its conversation id (e.g. across a cleared or resumed conversation). It may
// be empty when the bridge runs outside herdr.
func SnapshotFromStatusLine(payload []byte, paneID string, nowMs int64) (Snapshot, error) {
	if len(strings.TrimSpace(string(payload))) == 0 {
		return Snapshot{}, ErrMalformedPayload
	}
	var parsed statusLinePayload
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return Snapshot{}, ErrMalformedPayload
	}
	if parsed.ConversationID == "" {
		return Snapshot{}, ErrNoSessionID
	}
	if parsed.ContextWindow == nil {
		return Snapshot{}, ErrNoContextWindow
	}

	snap := Snapshot{
		SessionID:     parsed.ConversationID,
		PaneID:        paneID,
		Model:         modelLabel(parsed),
		Cwd:           workingDir(parsed),
		ContextTokens: parsed.ContextWindow.TotalInputTokens,
		PlanTier:      parsed.PlanTier,
		Email:         parsed.Email,
		UpdatedAtMs:   nowMs,
	}
	// A zero or absent window size is not a window: reporting it as one would
	// render a meaningless 0% occupancy instead of the bare token count.
	if size := parsed.ContextWindow.ContextWindowSize; size != nil && *size > 0 {
		window := *size
		snap.WindowTokens = &window
	}
	if cu := parsed.ContextWindow.CurrentUsage; cu != nil {
		snap.Cache = core.CacheFromTokenCounts(cu.InputTokens, cu.CacheReadInputTokens, cu.CacheCreationInputTokens)
	}
	if len(parsed.Quota) > 0 {
		snap.Quota = make(map[string]QuotaWindow, len(parsed.Quota))
		for id, w := range parsed.Quota {
			snap.Quota[id] = QuotaWindow{RemainingFraction: w.RemainingFraction, ResetTime: w.ResetTime}
		}
	}
	return snap, nil
}

// modelLabel prefers the human display name, falling back to the raw model
// id when Antigravity omits one.
func modelLabel(parsed statusLinePayload) string {
	if parsed.Model.DisplayName != "" {
		return parsed.Model.DisplayName
	}
	return parsed.Model.ID
}

// workingDir prefers the top-level cwd, which matches workspace.current_dir
// in every captured payload; the nested field is the fallback.
func workingDir(parsed statusLinePayload) string {
	if parsed.Cwd != "" {
		return parsed.Cwd
	}
	return parsed.Workspace.CurrentDir
}
