/**
 * Tests for the Claude limits cache.
 */
package limits

import (
	"encoding/json"
	"fmt"
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

func TestClaudeLimitsCache_PrefersFresherStatusLine(t *testing.T) {
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
	if limits.Primary == nil || limits.Primary.UsedPercentage != 99 {
		t.Fatalf("%+v", limits.Primary)
	}
	if limits.Source != "claude statusLine cache" {
		t.Fatalf("source=%q", limits.Source)
	}
}

func TestClaudeLimitsCache_PrefersFresherJSON(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")
	jsonPath := filepath.Join(dir, "claude.json")
	_ = os.WriteFile(jsonPath, []byte(`{
		"cachedUsageUtilization": {
			"fetchedAtMs": 5000,
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
	}, 1_000, cachePath)

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

func TestWriteClaudeLimitsCacheGuarded_StoresPromptCacheExpiry(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")
	expires := int64(1_700_003_600)
	input := fiveHourInput(42)
	input.PromptCachePresent = true
	input.PromptCacheExpiresAt = &expires
	if wrote, err := WriteClaudeLimitsCacheGuarded(input, 1_000, cachePath); err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	got := ReadPromptCacheExpiresAt(cachePath)
	if got == nil || *got != expires {
		t.Fatalf("expires=%v", got)
	}

	cold := RateLimitsInput{PromptCachePresent: true}
	if wrote, err := WriteClaudeLimitsCacheGuarded(cold, 2_000, cachePath); err != nil || !wrote {
		t.Fatalf("cold write: wrote=%v err=%v", wrote, err)
	}
	if ReadPromptCacheExpiresAt(cachePath) != nil {
		t.Fatal("cold prefix must clear recorded expiry")
	}
	limits := CollectClaudeLimits(3_000, CollectClaudeLimitsOptions{
		StatusLineCachePath: cachePath,
		ClaudeJSONPath:      filepath.Join(dir, "missing.json"),
	})
	if limits.Primary == nil || limits.Primary.UsedPercentage != 42 {
		t.Fatalf("windows must survive a prompt_cache-only write: %+v", limits.Primary)
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

// Two distinct Claude accounts as OMP records them. The composed account_key
// is the shape OMP writes; it is deliberately not an identity (see
// AccountWindows.identity), so the strict-join tests below must key off the
// email/account id inside it.
const (
	claudeObsEmailA = "asrsena3302@gmail.com"
	claudeObsIDA    = "f3a39003-3c6f-438d-8b25-cf345f085528"
	claudeObsKeyA   = "oauth|account:f3a39003-3c6f-438d-8b25-cf345f085528|email:asrsena3302@gmail.com|org:1ee9778e-5379-4a19-a14a-2625ea6002d5"

	claudeObsEmailB = "second.account@example.com"
	claudeObsIDB    = "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"
	claudeObsKeyB   = "oauth|account:0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9|email:second.account@example.com|org:7c6b5a49-3821-4f0e-9d1c-2b3a4958e6f7"
)

// claudeObservationRows is the pair of usage_history rows OMP writes for one
// Claude account: the 5h and 7d windows the server reported. Fractions are
// binary-exact so the ×100 conversion is comparable without a tolerance.
func claudeObservationRows(recordedAtMs int64, accountKey, email, accountID string, fiveHour, sevenDay float64) []usageRow {
	return []usageRow{
		{
			RecordedAtMs: recordedAtMs, Provider: "anthropic", AccountKey: accountKey,
			Email: email, AccountID: accountID, LimitID: "anthropic:5h",
			Label: "Claude", WindowLabel: "5h", UsedFraction: fiveHour,
			Status: "ok", ResetsAtMs: recordedAtMs + 3_600_000,
		},
		{
			RecordedAtMs: recordedAtMs, Provider: "anthropic", AccountKey: accountKey,
			Email: email, AccountID: accountID, LimitID: "anthropic:7d",
			Label: "Claude", WindowLabel: "7d", UsedFraction: sevenDay,
			Status: "ok", ResetsAtMs: recordedAtMs + 86_400_000,
		},
	}
}

func writeClaudeJSONFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mustHaveBorrowableObservation asserts the pool really holds an observation
// this collector could borrow. A test that expects borrowing NOT to happen is
// worthless without it: it would pass just as happily when the fixture DB
// never loaded at all.
func mustHaveBorrowableObservation(t *testing.T, providerID string) {
	t.Helper()
	if SelectAccountWindows(ObserveAccountWindows(), providerID, "") == nil {
		t.Fatalf("fixture holds no borrowable %s observation", providerID)
	}
}

// Issue #29: a Claude subscription driven entirely through another harness
// writes neither Claude Code artifact, so both first-hand tiers are empty.
// The windows still belong to the account, and OMP observed them — reporting
// "no data" here was the regression.
func TestClaudeLimitsCache_BorrowsObservationWhenNoArtifacts(t *testing.T) {
	const nowMs = int64(1_800_000_000_000)
	const observedAtMs = nowMs - 5*60_000
	dir := t.TempDir()
	useAgentDB(t, claudeObservationRows(observedAtMs, claudeObsKeyA, claudeObsEmailA, claudeObsIDA, 0.25, 0.75)...)

	got := CollectClaudeLimits(nowMs, CollectClaudeLimitsOptions{
		StatusLineCachePath: filepath.Join(dir, "missing-cache.json"),
		ClaudeJSONPath:      filepath.Join(dir, "missing-claude.json"),
	})

	if got.Source != "omp usage_history" {
		t.Fatalf("source = %q, want borrowed observation", got.Source)
	}
	if got.Primary == nil || got.Primary.UsedPercentage != 25 {
		t.Fatalf("primary = %+v, want the borrowed 5h window at 25%%", got.Primary)
	}
	if got.Secondary == nil || got.Secondary.UsedPercentage != 75 {
		t.Fatalf("secondary = %+v, want the borrowed 7d window at 75%%", got.Secondary)
	}
	if got.Note == nil {
		t.Fatal("borrowed windows must be attributed, got no note")
	}
	if !containsStr(*got.Note, "via OMP") {
		t.Fatalf("note = %q, want observer attribution", *got.Note)
	}
	if !containsStr(*got.Note, claudeObsEmailA) {
		t.Fatalf("note = %q, want the observed account named", *got.Note)
	}
	if got.FetchedAtMs != observedAtMs {
		t.Fatalf("fetchedAtMs = %d, want the observation time %d", got.FetchedAtMs, observedAtMs)
	}
}

// A first-hand Claude artifact always outranks a borrowed observation: the
// account's own CLI wrote its numbers, the pool only relayed someone else's.
func TestClaudeLimitsCache_FirstHandOutranksBorrowed(t *testing.T) {
	const nowMs = int64(1_800_000_000_000)
	cases := []struct {
		name       string
		writeJSON  bool
		writeCache bool
		wantPct    float64
		wantSource string
	}{
		{"claude.json utilization wins", true, false, 8, "claude.json cachedUsageUtilization"},
		{"statusLine cache wins", false, true, 61, "claude statusLine cache"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			jsonPath := filepath.Join(dir, "claude.json")
			cachePath := filepath.Join(dir, "cache.json")
			// An observation is available and must lose to either artifact.
			useAgentDB(t, claudeObservationRows(nowMs-5*60_000, claudeObsKeyA, claudeObsEmailA, claudeObsIDA, 0.5, 0.5)...)
			mustHaveBorrowableObservation(t, "claude")

			if tc.writeJSON {
				writeClaudeJSONFile(t, jsonPath, `{
					"cachedUsageUtilization": {
						"fetchedAtMs": 1799999940000,
						"utilization": {
							"five_hour": { "utilization": 8, "resets_at": "2027-01-15T12:00:00.000Z" },
							"seven_day": { "utilization": 21, "resets_at": "2027-01-20T00:00:00.000Z" }
						}
					},
					"oauthAccount": { "emailAddress": "`+claudeObsEmailA+`" }
				}`)
			}
			if tc.writeCache {
				if err := WriteClaudeLimitsCache(fiveHourInput(61), nowMs, cachePath); err != nil {
					t.Fatal(err)
				}
			}

			got := CollectClaudeLimits(nowMs, CollectClaudeLimitsOptions{
				StatusLineCachePath: cachePath,
				ClaudeJSONPath:      jsonPath,
			})

			if got.Source != tc.wantSource {
				t.Fatalf("source = %q, want %q (first-hand must outrank borrowed)", got.Source, tc.wantSource)
			}
			if got.Primary == nil || got.Primary.UsedPercentage != tc.wantPct {
				t.Fatalf("primary = %+v, want the first-hand %v%%", got.Primary, tc.wantPct)
			}
			if got.Note != nil {
				t.Fatalf("note = %q, want none for a fresh first-hand read", *got.Note)
			}
		})
	}
}

// ~/.claude.json names the account this pane bills to even when it carries no
// utilization. Borrowing is then a strict join: another account's numbers are
// worse than no numbers, because they look first-hand once rendered.
func TestClaudeLimitsCache_BorrowRequiresMatchingAccount(t *testing.T) {
	const nowMs = int64(1_800_000_000_000)
	cases := []struct {
		name                           string
		obsKey, obsEmail, obsAccountID string
		wantBorrow                     bool
	}{
		{"observation belongs to another account", claudeObsKeyB, claudeObsEmailB, claudeObsIDB, false},
		{"observation belongs to this account", claudeObsKeyA, claudeObsEmailA, claudeObsIDA, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			jsonPath := filepath.Join(dir, "claude.json")
			// Logged in as account A, but Claude Code never cached a window.
			writeClaudeJSONFile(t, jsonPath, `{"oauthAccount": {"emailAddress": "`+claudeObsEmailA+`"}}`)
			useAgentDB(t, claudeObservationRows(nowMs-5*60_000, tc.obsKey, tc.obsEmail, tc.obsAccountID, 0.25, 0.75)...)
			mustHaveBorrowableObservation(t, "claude")

			got := CollectClaudeLimits(nowMs, CollectClaudeLimitsOptions{
				StatusLineCachePath: filepath.Join(dir, "missing-cache.json"),
				ClaudeJSONPath:      jsonPath,
			})

			if !tc.wantBorrow {
				if got.Primary != nil || got.Secondary != nil {
					t.Fatalf("borrowed another account's windows: %+v / %+v", got.Primary, got.Secondary)
				}
				if got.Source != "none" {
					t.Fatalf("source = %q, want none", got.Source)
				}
				if got.Note == nil || !containsStr(*got.Note, "no ~/.claude.json utilization") {
					t.Fatalf("note = %v, want the no-utilization fallback", got.Note)
				}
				return
			}
			if got.Source != "omp usage_history" {
				t.Fatalf("source = %q, want borrowed observation", got.Source)
			}
			if got.Primary == nil || got.Primary.UsedPercentage != 25 {
				t.Fatalf("primary = %+v, want the borrowed 5h window at 25%%", got.Primary)
			}
			if got.Note == nil || !containsStr(*got.Note, "via OMP") {
				t.Fatalf("note = %v, want observer attribution", got.Note)
			}
		})
	}
}

// claudeJSONWithFetchedAt is a ~/.claude.json whose cached utilization carries
// an explicit observation time, so recency can be pitted against provenance.
func claudeJSONWithFetchedAt(fetchedAtMs int64, fiveHourUsed float64) string {
	return fmt.Sprintf(`{
		"oauthAccount": {"emailAddress": %q},
		"cachedUsageUtilization": {
			"fetchedAtMs": %d,
			"utilization": {
				"five_hour": {"utilization": %v, "resets_at": "2026-07-15T00:00:00.000Z"},
				"seven_day": {"utilization": 5, "resets_at": "2026-07-20T00:00:00.000Z"}
			}
		}
	}`, claudeObsEmailA, fetchedAtMs, fiveHourUsed)
}

// TestClaudeLimitsCache_FreshestObservationWins pins the rule that provenance
// does not decide the reading — the timestamp does.
//
// Observed live: ~/.claude.json reported 43% used while OMP reported 80% for
// the SAME 5h window (identical resets_at), because the Claude CLI had been
// idle while OMP kept working. Preferring the first-hand cache there
// under-reports usage right up against a limit, which is the failure this
// guards.
func TestClaudeLimitsCache_FreshestObservationWins(t *testing.T) {
	const nowMs = 1_800_000_000_000
	const minute = 60_000

	cases := []struct {
		name           string
		jsonFetchedAt  int64
		observedAt     int64
		wantSource     string
		wantPrimaryPct float64
	}{
		{
			name:           "stale claude.json loses to a fresher observation",
			jsonFetchedAt:  nowMs - 60*minute,
			observedAt:     nowMs - 1*minute,
			wantSource:     "omp usage_history",
			wantPrimaryPct: 80,
		},
		{
			name:           "fresh claude.json beats an older observation",
			jsonFetchedAt:  nowMs - 1*minute,
			observedAt:     nowMs - 60*minute,
			wantSource:     "claude.json cachedUsageUtilization",
			wantPrimaryPct: 43,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			jsonPath := filepath.Join(dir, "claude.json")
			writeClaudeJSONFile(t, jsonPath, claudeJSONWithFetchedAt(tc.jsonFetchedAt, 43))
			useAgentDB(t, claudeObservationRows(
				tc.observedAt, claudeObsKeyA, claudeObsEmailA, claudeObsIDA, 0.8, 0.25)...)
			mustHaveBorrowableObservation(t, "claude")

			got := CollectClaudeLimits(nowMs, CollectClaudeLimitsOptions{
				StatusLineCachePath: filepath.Join(dir, "missing-cache.json"),
				ClaudeJSONPath:      jsonPath,
			})
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
