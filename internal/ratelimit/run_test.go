// Tests configured-threshold notification execution from statusline input.
package ratelimit

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunRateLimitCheckWithThresholdsInUsesConfiguredBuckets ensures the
// statusline execution path does not fall back to the fixed 20% bucket.
func TestRunRateLimitCheckWithThresholdsInUsesConfiguredBuckets(t *testing.T) {
	dir := t.TempDir()
	var notifications []string
	RunRateLimitCheckWithThresholdsIn(
		dir,
		`{"rate_limits":{"five_hour":{"used_percentage":78,"resets_at":1800000000}}}`,
		1_700_000_000_000,
		[]int{30},
		func(title, body string) bool {
			notifications = append(notifications, title+": "+body)
			return true
		},
	)

	if len(notifications) != 1 || notifications[0] != "Session limit: 30% remaining · resets in 1157d 9h" {
		t.Fatalf("notifications=%v", notifications)
	}
	if _, err := os.Stat(filepath.Join(dir, "rate-limit-state.json")); err != nil {
		t.Fatal(err)
	}
}
