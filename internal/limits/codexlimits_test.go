/**
 * Tests for Codex rate_limits extraction.
 */
package limits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tokenCountRateLine(primary, secondary map[string]any, plan string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{
				"last_token_usage":     map[string]any{"total_tokens": 100},
				"model_context_window": 200_000,
			},
			"rate_limits": map[string]any{
				"primary": primary, "secondary": secondary, "plan_type": plan,
			},
		},
	})
	return string(b)
}

func TestExtractRateLimitsFromLines(t *testing.T) {
	lines := []string{tokenCountRateLine(
		map[string]any{"used_percent": 3, "window_minutes": 300, "resets_at": 100},
		map[string]any{"used_percent": 10, "window_minutes": 10080, "resets_at": 200},
		"plus",
	)}
	got := ExtractRateLimitsFromLines(lines)
	if got == nil || got.Primary == nil || got.Primary.UsedPercentage != 3 {
		t.Fatalf("%+v", got)
	}
	if got.Primary.ResetsAt == nil || *got.Primary.ResetsAt != 100 {
		t.Fatalf("primary resets %+v", got.Primary)
	}
	if got.Secondary == nil || got.Secondary.UsedPercentage != 10 {
		t.Fatalf("secondary %+v", got.Secondary)
	}
	if got.PlanType == nil || *got.PlanType != "plus" {
		t.Fatalf("plan %+v", got.PlanType)
	}
}

func TestExtractRateLimitsFromLines_Latest(t *testing.T) {
	lines := []string{
		tokenCountRateLine(map[string]any{"used_percent": 1, "window_minutes": 300, "resets_at": 1}, nil, "plus"),
		tokenCountRateLine(
			map[string]any{"used_percent": 50, "window_minutes": 300, "resets_at": 2},
			map[string]any{"used_percent": 20, "window_minutes": 10080, "resets_at": 3},
			"plus",
		),
	}
	got := ExtractRateLimitsFromLines(lines)
	if got == nil || got.Primary == nil || got.Primary.UsedPercentage != 50 {
		t.Fatalf("%+v", got)
	}
	if got.Secondary == nil || got.Secondary.UsedPercentage != 20 {
		t.Fatalf("%+v", got.Secondary)
	}
}

func TestExtractRateLimitsFromLines_Missing(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{"last_token_usage": map[string]any{"total_tokens": 1}},
		},
	})
	if ExtractRateLimitsFromLines([]string{string(b)}) != nil {
		t.Fatal("expected nil")
	}
}

// sessionMetaLine is the first line of a rollout, carrying the session cwd.
func sessionMetaLine(cwd string) string {
	b, _ := json.Marshal(map[string]any{
		"type":    "session_meta",
		"payload": map[string]any{"cwd": cwd, "session_id": "sid-" + cwd, "id": "id-" + cwd},
	})
	return string(b)
}

// writeRollout writes a rollout jsonl under CODEX_HOME/sessions and stamps its
// mtime (ListNewestRolloutPaths orders by mtime). When usedPercent < 0, the file
// holds only session_meta (a just-opened session with no token_count yet).
func writeRollout(t *testing.T, root, name, cwd string, usedPercent float64, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(root, "sessions", "2026", "07", "18")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := sessionMetaLine(cwd) + "\n"
	if usedPercent >= 0 {
		content += tokenCountRateLine(
			map[string]any{"used_percent": usedPercent, "window_minutes": 10080, "resets_at": 1784816502},
			nil, "plus",
		) + "\n"
	}
	path := filepath.Join(dir, "rollout-2026-07-18T00-00-00-"+name+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// Codex rate limits are account-global. Two panes with different cwds must both
// report the freshest snapshot, never their own (possibly stale) session — the
// bug where an idle tab showed a lower % than the active tab.
func TestCollectCodexLimits_AccountGlobalCwdIndependent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	now := time.Now()
	writeRollout(t, root, "projB-stale", "/repo/projectB", 2, now.Add(-2*time.Hour))
	writeRollout(t, root, "projA-fresh", "/repo/projectA", 6, now)

	cwdA, cwdB := "/repo/projectA", "/repo/projectB"
	gotA := CollectCodexLimits(&cwdA, 1000)
	gotB := CollectCodexLimits(&cwdB, 1000)

	if gotA.Primary == nil || gotA.Primary.UsedPercentage != 6 {
		t.Fatalf("cwdA primary = %+v, want freshest 6", gotA.Primary)
	}
	if gotB.Primary == nil || gotB.Primary.UsedPercentage != 6 {
		t.Fatalf("cwdB primary = %+v, want freshest 6 (must not read projectB's stale 2%%)", gotB.Primary)
	}
	if gotA.Primary.UsedPercentage != gotB.Primary.UsedPercentage {
		t.Fatalf("cwd divergence: A=%v B=%v", gotA.Primary.UsedPercentage, gotB.Primary.UsedPercentage)
	}
}

// The newest rollout by mtime may be a just-opened session with no token_count
// yet; the collector must fall through to the newest one that has a snapshot.
func TestCollectCodexLimits_SkipsSnapshotlessNewest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	now := time.Now()
	writeRollout(t, root, "has-data", "/repo/a", 7, now.Add(-time.Hour))
	writeRollout(t, root, "meta-only", "/repo/b", -1, now) // newest, no rate_limits

	got := CollectCodexLimits(nil, 1000)
	if got.Primary == nil || got.Primary.UsedPercentage != 7 {
		t.Fatalf("expected fallback to older snapshot 7, got %+v (note=%v)", got.Primary, got.Note)
	}
}
