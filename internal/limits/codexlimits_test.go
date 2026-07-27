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

// codexObservationRows is the pair of usage_history rows OMP writes for one
// Codex account. Fractions are binary-exact so the ×100 conversion compares
// without a tolerance.
func codexObservationRows(recordedAtMs int64, primary, secondary float64) []usageRow {
	const (
		accountKey = "oauth|account:9f8e7d6c-5b4a-4392-8170-6e5d4c3b2a19|email:asrsena3302@gmail.com"
		email      = "asrsena3302@gmail.com"
		accountID  = "9f8e7d6c-5b4a-4392-8170-6e5d4c3b2a19"
	)
	return []usageRow{
		{
			RecordedAtMs: recordedAtMs, Provider: "openai-codex", AccountKey: accountKey,
			Email: email, AccountID: accountID, LimitID: "openai-codex:primary",
			Label: "Codex", WindowLabel: "5h", UsedFraction: primary,
			Status: "ok", ResetsAtMs: recordedAtMs + 3_600_000,
		},
		{
			RecordedAtMs: recordedAtMs, Provider: "openai-codex", AccountKey: accountKey,
			Email: email, AccountID: accountID, LimitID: "openai-codex:secondary",
			Label: "Codex", WindowLabel: "weekly", UsedFraction: secondary,
			Status: "ok", ResetsAtMs: recordedAtMs + 86_400_000,
		},
	}
}

// Issue #29: a Codex subscription driven through another harness leaves
// ~/.codex/sessions empty, so the only local record of the account's windows
// is another agent's observation.
func TestCollectCodexLimits_BorrowsObservationWhenNoRollouts(t *testing.T) {
	base := time.Now().Truncate(time.Second)
	nowMs := base.UnixMilli()
	observedAtMs := nowMs - 5*60_000
	t.Setenv("CODEX_HOME", t.TempDir())
	useAgentDB(t, codexObservationRows(observedAtMs, 0.25, 0.75)...)

	got := CollectCodexLimits(nil, nowMs)

	if got.Source != "omp usage_history" {
		t.Fatalf("source = %q, want borrowed observation", got.Source)
	}
	if got.Primary == nil || got.Primary.UsedPercentage != 25 {
		t.Fatalf("primary = %+v, want the borrowed primary window at 25%%", got.Primary)
	}
	if got.Secondary == nil || got.Secondary.UsedPercentage != 75 {
		t.Fatalf("secondary = %+v, want the borrowed secondary window at 75%%", got.Secondary)
	}
	if got.Note == nil || !containsStr(*got.Note, "via OMP") {
		t.Fatalf("note = %v, want observer attribution", got.Note)
	}
	if got.FetchedAtMs != observedAtMs {
		t.Fatalf("fetchedAtMs = %d, want the observation time %d", got.FetchedAtMs, observedAtMs)
	}
}

// A rollout snapshot is first-hand and must outrank a borrowed observation.
func TestCollectCodexLimits_RolloutOutranksBorrowed(t *testing.T) {
	base := time.Now().Truncate(time.Second)
	nowMs := base.UnixMilli()
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	useAgentDB(t, codexObservationRows(nowMs-5*60_000, 0.25, 0.75)...)
	mustHaveBorrowableObservation(t, "codex")
	writeRollout(t, root, "has-data", "/repo/a", 8, base)

	got := CollectCodexLimits(nil, nowMs)

	if got.Source != "codex rollout" {
		t.Fatalf("source = %q, want the first-hand rollout", got.Source)
	}
	if got.Primary == nil || got.Primary.UsedPercentage != 8 {
		t.Fatalf("primary = %+v, want the rollout's 8%%, not the borrowed 25%%", got.Primary)
	}
}

// Sessions exist but none ever recorded a token_count, so there is no
// first-hand snapshot to prefer — the observation is all there is.
func TestCollectCodexLimits_BorrowsWhenRolloutsCarryNoRateLimits(t *testing.T) {
	base := time.Now().Truncate(time.Second)
	nowMs := base.UnixMilli()
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	useAgentDB(t, codexObservationRows(nowMs-5*60_000, 0.25, 0.75)...)
	writeRollout(t, root, "meta-only-a", "/repo/a", -1, base.Add(-time.Minute))
	writeRollout(t, root, "meta-only-b", "/repo/b", -1, base)

	got := CollectCodexLimits(nil, nowMs)

	if got.Source != "omp usage_history" {
		t.Fatalf("source = %q, want borrowed observation", got.Source)
	}
	if got.Primary == nil || got.Primary.UsedPercentage != 25 {
		t.Fatalf("primary = %+v, want the borrowed primary window at 25%%", got.Primary)
	}
}

// With neither a rollout nor an observation the collector still reports the
// plain "no data" row rather than inventing one.
func TestCollectCodexLimits_NoRolloutsAndNoObservation(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())

	got := CollectCodexLimits(nil, time.Now().UnixMilli())

	if got.Source != "none" {
		t.Fatalf("source = %q, want none", got.Source)
	}
	if got.Primary != nil || got.Secondary != nil {
		t.Fatalf("expected no windows, got %+v / %+v", got.Primary, got.Secondary)
	}
	if got.Note == nil || *got.Note != "no rollout jsonl under ~/.codex/sessions" {
		t.Fatalf("note = %v, want the missing-rollout explanation", got.Note)
	}
}

// A rollout is only as current as the turn that wrote it. Past
// codexStaleAfterMinutes the snapshot is labeled rather than rendered as if it
// were live; a fresh one must carry no warning at all.
func TestCollectCodexLimits_StaleRolloutIsLabeled(t *testing.T) {
	base := time.Now().Truncate(time.Second)
	nowMs := base.UnixMilli()
	cases := []struct {
		name     string
		mtime    time.Time
		wantNote string // "" means the note must be absent
	}{
		{"two-hour-old snapshot is labeled stale", base.Add(-2 * time.Hour), "stale ~120m ago"},
		{"snapshot from this turn is not labeled", base, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("CODEX_HOME", root)
			writeRollout(t, root, "only", "/repo/a", 12, tc.mtime)

			got := CollectCodexLimits(nil, nowMs)

			if got.Primary == nil || got.Primary.UsedPercentage != 12 {
				t.Fatalf("primary = %+v, want the rollout snapshot regardless of age", got.Primary)
			}
			if tc.wantNote == "" {
				if got.Note != nil {
					t.Fatalf("note = %q, want none for a current snapshot", *got.Note)
				}
				return
			}
			if got.Note == nil {
				t.Fatalf("want note %q, got none", tc.wantNote)
			}
			if *got.Note != tc.wantNote {
				t.Fatalf("note = %q, want %q", *got.Note, tc.wantNote)
			}
		})
	}
}

// TestCollectCodexLimits_FreshestObservationWins pins recency over provenance
// for Codex: a rollout is a snapshot stamped at the turn that wrote it, so an
// account since driven by another agent has a newer reading elsewhere.
func TestCollectCodexLimits_FreshestObservationWins(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	nowMs := now.UnixMilli()

	cases := []struct {
		name           string
		rolloutMtime   time.Time
		observedAt     int64
		wantSource     string
		wantPrimaryPct float64
	}{
		{
			name:           "stale rollout loses to a fresher observation",
			rolloutMtime:   now.Add(-2 * time.Hour),
			observedAt:     nowMs - 60_000,
			wantSource:     "omp usage_history",
			wantPrimaryPct: 75,
		},
		{
			name:           "fresh rollout beats an older observation",
			rolloutMtime:   now,
			observedAt:     nowMs - 2*60*60_000,
			wantSource:     "codex rollout",
			wantPrimaryPct: 12,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("CODEX_HOME", root)
			writeRollout(t, root, "has-data", "/repo/a", 12, tc.rolloutMtime)
			useAgentDB(t, codexObservationRows(tc.observedAt, 0.75, 0.5)...)
			mustHaveBorrowableObservation(t, "codex")

			got := CollectCodexLimits(nil, nowMs)
			if got.Source != tc.wantSource {
				t.Fatalf("source = %q, want %q (note=%v)", got.Source, tc.wantSource, got.Note)
			}
			if got.Primary == nil {
				t.Fatal("expected a primary window")
			}
			if got.Primary.UsedPercentage != tc.wantPrimaryPct {
				t.Fatalf("primary used = %v%%, want %v%%", got.Primary.UsedPercentage, tc.wantPrimaryPct)
			}
		})
	}
}
