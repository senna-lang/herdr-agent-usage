/**
 * Tests for the Antigravity statusLine payload adapter.
 */
package antigravity

import (
	"errors"
	"testing"
)

// capturedPayload is a real Antigravity CLI 1.2.3 statusLine payload captured
// after one completed turn, with identifiers and the email redacted. Field
// names and every usage/quota value are verbatim.
const capturedPayload = `{
  "cwd": "/redacted/project",
  "session_id": "11111111-2222-3333-4444-555555555555",
  "conversation_id": "11111111-2222-3333-4444-555555555555",
  "transcript_path": "/redacted/brain/11111111-2222-3333-4444-555555555555/.system_generated/logs/transcript.jsonl",
  "model": { "id": "Gemini 3.8 Flash (High)", "display_name": "Gemini 3.8 Flash (High)", "effort": "high" },
  "workspace": { "current_dir": "/redacted/project", "project_dir": "/redacted/project" },
  "version": "1.2.3",
  "context_window": {
    "total_input_tokens": 18558,
    "total_output_tokens": 78,
    "context_window_size": 1048576,
    "used_percentage": 1.7698287963867188,
    "remaining_percentage": 98.23017120361328,
    "current_usage": {
      "input_tokens": 13025,
      "output_tokens": 78,
      "cache_creation_input_tokens": 0,
      "cache_read_input_tokens": 0
    }
  },
  "exceeds_200k_tokens": false,
  "product": "antigravity",
  "quota": {
    "3p-weekly": { "remaining_fraction": 1, "reset_time": "2026-09-22T10:11:18Z", "reset_in_seconds": 604799 },
    "gemini-weekly": { "remaining_fraction": 0.9920056, "reset_time": "2026-09-22T10:06:55Z", "reset_in_seconds": 604536 }
  },
  "agent_state": "idle",
  "sandbox": { "enabled": false },
  "plan_tier": "Antigravity Starter Quota",
  "email": "redacted@example.com",
  "terminal_width": 204
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
	if snap.ContextTokens != 18558 {
		t.Errorf("ContextTokens = %d, want 18558", snap.ContextTokens)
	}
	if snap.WindowTokens == nil || *snap.WindowTokens != 1048576 {
		t.Errorf("WindowTokens = %v, want 1048576", snap.WindowTokens)
	}
	if snap.Model != "Gemini 3.8 Flash (High)" {
		t.Errorf("Model = %q", snap.Model)
	}
	if snap.Cwd != "/redacted/project" {
		t.Errorf("Cwd = %q", snap.Cwd)
	}
	if snap.PlanTier != "Antigravity Starter Quota" {
		t.Errorf("PlanTier = %q", snap.PlanTier)
	}
	if snap.Email != "redacted@example.com" {
		t.Errorf("Email = %q", snap.Email)
	}
	if snap.UpdatedAtMs != 1000 {
		t.Errorf("UpdatedAtMs = %d", snap.UpdatedAtMs)
	}
	geminiWeekly, ok := snap.Quota["gemini-weekly"]
	if !ok {
		t.Fatal("missing gemini-weekly quota bucket")
	}
	if geminiWeekly.RemainingFraction != 0.9920056 {
		t.Errorf("gemini-weekly RemainingFraction = %v", geminiWeekly.RemainingFraction)
	}
	if geminiWeekly.ResetTime != "2026-09-22T10:06:55Z" {
		t.Errorf("gemini-weekly ResetTime = %q", geminiWeekly.ResetTime)
	}
	thirdParty, ok := snap.Quota["3p-weekly"]
	if !ok {
		t.Fatal("missing 3p-weekly quota bucket")
	}
	if thirdParty.RemainingFraction != 1 {
		t.Errorf("3p-weekly RemainingFraction = %v", thirdParty.RemainingFraction)
	}
	// The captured turn's cache_creation/cache_read counters were both 0 (a
	// fresh conversation has nothing cached yet), but input_tokens (13025) is
	// the fresh, non-cached portion and is nonzero, so Cache is populated
	// with a 0% hit rate rather than nil.
	if snap.Cache == nil {
		t.Fatal("Cache = nil, want a populated cache observation")
	}
	if snap.Cache.FreshInputTokens != 13025 || snap.Cache.ReadTokens != 0 || snap.Cache.CreationTokens != 0 {
		t.Errorf("Cache = %+v", snap.Cache)
	}
}

// The captured payload's total_input_tokens is a plain, non-nullable integer
// (unlike Cursor, which nulls the field out early in a session): a
// legitimately-zero token count before the first turn must not be rejected.
func TestSnapshotFromStatusLine_ZeroTokensBeforeFirstTurn(t *testing.T) {
	payload := `{
		"conversation_id": "11111111-2222-3333-4444-555555555555",
		"cwd": "/redacted/project",
		"context_window": {
			"total_input_tokens": 0,
			"context_window_size": 1048576,
			"current_usage": null
		}
	}`
	snap, err := SnapshotFromStatusLine([]byte(payload), "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ContextTokens != 0 {
		t.Errorf("ContextTokens = %d, want 0", snap.ContextTokens)
	}
	if snap.Cache != nil {
		t.Errorf("Cache = %+v, want nil when current_usage is null", snap.Cache)
	}
}

// current_usage's input_tokens/cache_read/cache_creation feed Cache directly
// once a turn has produced cache-bearing traffic. Synthetic, since neither
// captured payload observed a nonzero cache turn.
func TestSnapshotFromStatusLine_CacheFromCurrentUsage(t *testing.T) {
	payload := `{
		"conversation_id": "11111111-2222-3333-4444-555555555555",
		"context_window": {
			"total_input_tokens": 5000,
			"context_window_size": 1048576,
			"current_usage": {
				"input_tokens": 100,
				"cache_creation_input_tokens": 50,
				"cache_read_input_tokens": 850
			}
		}
	}`
	snap, err := SnapshotFromStatusLine([]byte(payload), "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Cache == nil {
		t.Fatal("Cache = nil, want a populated cache observation")
	}
	if snap.Cache.FreshInputTokens != 100 || snap.Cache.ReadTokens != 850 || snap.Cache.CreationTokens != 50 {
		t.Fatalf("Cache = %+v", snap.Cache)
	}
}

func TestSnapshotFromStatusLine_Rejections(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		wantErr error
	}{
		{"empty", "", ErrMalformedPayload},
		{"blank", "   ", ErrMalformedPayload},
		{"garbage", "not json", ErrMalformedPayload},
		{"truncated", `{"conversation_id":"x","context_window":{`, ErrMalformedPayload},
		{"missing conversation_id", `{"context_window":{"total_input_tokens":0}}`, ErrNoSessionID},
		{"empty conversation_id", `{"conversation_id":"","context_window":{"total_input_tokens":0}}`, ErrNoSessionID},
		{"missing context_window", `{"conversation_id":"x"}`, ErrNoContextWindow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SnapshotFromStatusLine([]byte(tc.payload), "", 0)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// Early in a session Antigravity could in principle report tokens without a
// window size; the token count is still usable, and a zero window is not a
// window.
func TestSnapshotFromStatusLine_MissingWindowSize(t *testing.T) {
	payload := `{
		"conversation_id": "11111111-2222-3333-4444-555555555555",
		"context_window": { "total_input_tokens": 500 }
	}`
	snap, err := SnapshotFromStatusLine([]byte(payload), "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ContextTokens != 500 {
		t.Errorf("ContextTokens = %d, want 500", snap.ContextTokens)
	}
	if snap.WindowTokens != nil {
		t.Errorf("WindowTokens = %v, want nil", snap.WindowTokens)
	}
}

func TestSnapshotFromStatusLine_LabelFallbacks(t *testing.T) {
	payload := `{
		"conversation_id": "11111111-2222-3333-4444-555555555555",
		"workspace": { "current_dir": "/from/workspace" },
		"model": { "id": "raw-id" },
		"context_window": { "total_input_tokens": 1 }
	}`
	snap, err := SnapshotFromStatusLine([]byte(payload), "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Model != "raw-id" {
		t.Errorf("Model = %q, want fallback to raw id", snap.Model)
	}
	if snap.Cwd != "/from/workspace" {
		t.Errorf("Cwd = %q, want fallback to workspace.current_dir", snap.Cwd)
	}
}
