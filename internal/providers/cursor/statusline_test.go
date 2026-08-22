/**
 * Tests for the Cursor statusLine payload adapter.
 */
package cursor

import (
	"errors"
	"testing"
)

// capturedPayload is a real Cursor CLI 2026.08.11-e8db854 statusLine payload
// with identifiers redacted. Field names and every usage value are verbatim.
// See docs/cursor-contract.md for the recorded contract this pins.
const capturedPayload = `{
  "session_id": "11111111-2222-3333-4444-555555555555",
  "transcript_path": "/redacted/transcript.jsonl",
  "render_width_chars": 394,
  "cwd": "/redacted/project",
  "autorun": false,
  "model": { "id": "default", "display_name": "Auto" },
  "workspace": { "current_dir": "/redacted/project", "project_dir": "/redacted/project/.cursor", "added_dirs": [] },
  "version": "2026.08.11-e8db854",
  "output_style": { "name": "compact" },
  "context_window": {
    "total_input_tokens": 42752,
    "total_output_tokens": 141,
    "context_window_size": 256000,
    "used_percentage": 16.7,
    "remaining_percentage": 83.3,
    "current_usage": {
      "input_tokens": 201, "output_tokens": 33,
      "cache_creation_input_tokens": 0, "cache_read_input_tokens": 42624
    }
  },
  "session_name": "redacted"
}`

func TestSnapshotFromStatusLine_CapturedPayload(t *testing.T) {
	snap, err := SnapshotFromStatusLine([]byte(capturedPayload), "w1:p1", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.SessionID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("SessionID = %q", snap.SessionID)
	}
	if snap.PaneID != "w1:p1" {
		t.Errorf("PaneID = %q", snap.PaneID)
	}
	// Occupancy is total_input_tokens, not any current_usage sum: the latter
	// describes the last API call (201+0+42624 = 42825 here).
	if snap.ContextTokens != 42752 {
		t.Errorf("ContextTokens = %d, want 42752", snap.ContextTokens)
	}
	if snap.WindowTokens == nil || *snap.WindowTokens != 256000 {
		t.Errorf("WindowTokens = %v, want 256000", snap.WindowTokens)
	}
	if snap.Model != "Auto" {
		t.Errorf("Model = %q", snap.Model)
	}
	if snap.Cwd != "/redacted/project" {
		t.Errorf("Cwd = %q", snap.Cwd)
	}
	if snap.UpdatedAtMs != 1000 {
		t.Errorf("UpdatedAtMs = %d", snap.UpdatedAtMs)
	}
}

// The captured payload pins Cursor's documented derivation: total_input_tokens
// is computed upstream from used_percentage, so the two agree by construction
// and the token count carries the percentage's quantisation.
func TestSnapshotFromStatusLine_TokensAgreeWithReportedPercentage(t *testing.T) {
	snap, err := SnapshotFromStatusLine([]byte(capturedPayload), "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const reportedPercentage = 16.7
	derived := int(reportedPercentage / 100 * float64(*snap.WindowTokens))
	if derived != snap.ContextTokens {
		t.Errorf("derived %d from %.1f%%, payload reports %d", derived, reportedPercentage, snap.ContextTokens)
	}
}

func TestSnapshotFromStatusLine_Rejections(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    error
	}{
		{"empty", "", ErrMalformedPayload},
		{"whitespace only", "   \n ", ErrMalformedPayload},
		{"not json", "garbage", ErrMalformedPayload},
		{"truncated json", `{"session_id":"a","context_window":{`, ErrMalformedPayload},
		{"no session id", `{"context_window":{"total_input_tokens":10}}`, ErrNoSessionID},
		{"null tokens", `{"session_id":"a","context_window":{"total_input_tokens":null}}`, ErrNoContextTokens},
		{"no context_window at all", `{"session_id":"a"}`, ErrNoContextTokens},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SnapshotFromStatusLine([]byte(tc.payload), "w1:p1", 1); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// Early in a session Cursor reports tokens without a window size. The token
// count is still usable; a zero window is not a window.
func TestSnapshotFromStatusLine_PartialWindow(t *testing.T) {
	for _, payload := range []string{
		`{"session_id":"a","context_window":{"total_input_tokens":900}}`,
		`{"session_id":"a","context_window":{"total_input_tokens":900,"context_window_size":null}}`,
		`{"session_id":"a","context_window":{"total_input_tokens":900,"context_window_size":0}}`,
	} {
		snap, err := SnapshotFromStatusLine([]byte(payload), "", 1)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", payload, err)
		}
		if snap.ContextTokens != 900 {
			t.Errorf("%s: ContextTokens = %d", payload, snap.ContextTokens)
		}
		if snap.WindowTokens != nil {
			t.Errorf("%s: WindowTokens = %v, want nil", payload, snap.WindowTokens)
		}
	}
}

func TestSnapshotFromStatusLine_LabelFallbacks(t *testing.T) {
	snap, err := SnapshotFromStatusLine([]byte(
		`{"session_id":"a","model":{"id":"composer-2"},"workspace":{"current_dir":"/w"},"context_window":{"total_input_tokens":5}}`), "", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Model != "composer-2" {
		t.Errorf("Model = %q, want the id when display_name is absent", snap.Model)
	}
	if snap.Cwd != "/w" {
		t.Errorf("Cwd = %q, want workspace.current_dir when cwd is absent", snap.Cwd)
	}
}
