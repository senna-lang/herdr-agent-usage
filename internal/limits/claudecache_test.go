/**
 * Tests for the Claude limits cache.
 */
package limits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeLimitsCache_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")
	missingJSON := filepath.Join(dir, "missing-claude.json")

	err := WriteClaudeLimitsCache(RateLimitsInput{
		FiveHour: &struct {
			UsedPercentage float64
			ResetsAt       int64
		}{30, 1000},
		SevenDay: &struct {
			UsedPercentage float64
			ResetsAt       int64
		}{50, 2000},
	}, 5_000, cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	b, _ := os.ReadFile(cachePath)
	_ = json.Unmarshal(b, &raw)
	five := raw["fiveHour"].(map[string]any)
	if five["usedPercentage"].(float64) != 30 {
		t.Fatalf("%v", five)
	}

	limits := CollectClaudeLimits(6_000, CollectClaudeLimitsOptions{
		StatusLineCachePath: cachePath,
		ClaudeJSONPath:      missingJSON,
	})
	if limits.Primary == nil || limits.Primary.UsedPercentage != 30 {
		t.Fatalf("%+v", limits.Primary)
	}
	if limits.Secondary == nil || limits.Secondary.UsedPercentage != 50 {
		t.Fatalf("%+v", limits.Secondary)
	}
	if limits.Source != "claude statusLine cache" {
		t.Fatalf("source=%q", limits.Source)
	}
}

func TestClaudeLimitsCache_PrefersJSON(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")
	jsonPath := filepath.Join(dir, "claude.json")
	_ = os.WriteFile(jsonPath, []byte(`{
		"cachedUsageUtilization": {
			"fetchedAtMs": 1,
			"utilization": {
				"five_hour": { "utilization": 1, "resets_at": "2026-07-15T00:00:00.000Z" },
				"seven_day": { "utilization": 2, "resets_at": "2026-07-20T00:00:00.000Z" }
			}
		}
	}`), 0o644)
	_ = WriteClaudeLimitsCache(RateLimitsInput{
		FiveHour: &struct {
			UsedPercentage float64
			ResetsAt       int64
		}{99, 1},
		SevenDay: &struct {
			UsedPercentage float64
			ResetsAt       int64
		}{99, 2},
	}, 5_000, cachePath)

	limits := CollectClaudeLimits(6_000, CollectClaudeLimitsOptions{
		StatusLineCachePath: cachePath,
		ClaudeJSONPath:      jsonPath,
	})
	if limits.Primary == nil || limits.Primary.UsedPercentage != 1 {
		t.Fatalf("%+v", limits.Primary)
	}
	if !containsStr(limits.Source, "cachedUsageUtilization") {
		t.Fatalf("source=%q", limits.Source)
	}
}

func TestClaudeLimitsCache_Neither(t *testing.T) {
	dir := t.TempDir()
	limits := CollectClaudeLimits(0, CollectClaudeLimitsOptions{
		StatusLineCachePath: filepath.Join(dir, "missing-cache.json"),
		ClaudeJSONPath:      filepath.Join(dir, "missing-claude.json"),
	})
	if limits.Primary != nil {
		t.Fatal("expected no primary")
	}
	if limits.Note == nil || !containsStr(*limits.Note, "no ~/.claude.json") {
		t.Fatalf("note=%v", limits.Note)
	}
}

func fiveHourInput(pct float64) RateLimitsInput {
	return RateLimitsInput{FiveHour: &struct {
		UsedPercentage float64
		ResetsAt       int64
	}{pct, 1000}}
}

func TestWriteClaudeLimitsCacheGuarded_SkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")

	// Seed a valid cache.
	if wrote, err := WriteClaudeLimitsCacheGuarded(fiveHourInput(42), 1_000, cachePath); err != nil || !wrote {
		t.Fatalf("seed write: wrote=%v err=%v", wrote, err)
	}
	// An empty payload must not clobber it.
	wrote, err := WriteClaudeLimitsCacheGuarded(RateLimitsInput{}, 2_000, cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Fatal("empty payload should not write")
	}
	got := CollectClaudeLimits(3_000, CollectClaudeLimitsOptions{
		StatusLineCachePath: cachePath,
		ClaudeJSONPath:      filepath.Join(dir, "missing.json"),
	})
	if got.Primary == nil || got.Primary.UsedPercentage != 42 {
		t.Fatalf("prior cache should survive, got %+v", got.Primary)
	}
}

func TestWriteClaudeLimitsCacheGuarded_SeparateProfilePaths(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a", "cache.json")
	pathB := filepath.Join(dir, "b", "cache.json")

	if _, err := WriteClaudeLimitsCacheGuarded(fiveHourInput(10), 1_000, pathA); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteClaudeLimitsCacheGuarded(fiveHourInput(90), 1_000, pathB); err != nil {
		t.Fatal(err)
	}
	a := CollectClaudeLimits(2_000, CollectClaudeLimitsOptions{StatusLineCachePath: pathA, ClaudeJSONPath: filepath.Join(dir, "na.json")})
	b := CollectClaudeLimits(2_000, CollectClaudeLimitsOptions{StatusLineCachePath: pathB, ClaudeJSONPath: filepath.Join(dir, "nb.json")})
	if a.Primary == nil || a.Primary.UsedPercentage != 10 {
		t.Fatalf("profile A = %+v", a.Primary)
	}
	if b.Primary == nil || b.Primary.UsedPercentage != 90 {
		t.Fatalf("profile B = %+v", b.Primary)
	}
}
