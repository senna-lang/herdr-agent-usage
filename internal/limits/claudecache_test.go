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
