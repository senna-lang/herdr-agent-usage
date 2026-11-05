/**
 * Tests for Devin limit collection.
 *
 * The property under test throughout: Devin reports the share of each plan
 * window the account has LEFT, so a window may only ever be drawn from a
 * real reading — fetched, decoded, and inverted — and every other case must
 * produce a note naming the cause rather than a fabricated bar.
 *
 * No test here reaches the network: the fetch is injected, and the cache is
 * isolated under a temp file.
 */
package limits

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/devin"
)

// devinCredentials writes one credentials.toml holding a login.
func devinCredentials(t *testing.T, apiKey string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.toml")
	raw := `windsurf_api_key = "` + apiKey + `"` + "\n" + `api_server_url = "https://server.example.test"` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// collectDevin runs the collector against a pinned fetch and an isolated
// cache, returning both the result and how often the fetch ran.
func collectDevin(t *testing.T, credentialsPath string, status *DevinPlanStatus, err error) (ProviderLimits, *int) {
	t.Helper()
	t.Setenv("USAGEBAR_DEVIN_CACHE_PATH", filepath.Join(t.TempDir(), "devin-quota.json"))
	calls := 0
	pl := CollectDevinLimits(nowMs, CollectDevinLimitsOptions{
		CredentialsPath: credentialsPath,
		FetchStatus: func(serverURL, apiKey string) (*DevinPlanStatus, error) {
			calls++
			if serverURL != "https://server.example.test" {
				t.Fatalf("fetch reached an unexpected host: %q", serverURL)
			}
			return status, err
		},
	})
	return pl, &calls
}

func proPlan() *DevinPlanStatus {
	daily, weekly := 42.0, 67.0
	resets := int64(1_793_520_000)
	return &DevinPlanStatus{
		PlanName:               "Pro",
		DailyRemainingPercent:  &daily,
		DailyResetsAt:          &resets,
		WeeklyRemainingPercent: &weekly,
		WeeklyResetsAt:         &resets,
		OverageBalanceMicros:   -318375,
		HasOverageBalance:      true,
	}
}

func TestCollectDevinLimits_NoLoginExplainsHowToFix(t *testing.T) {
	pl, calls := collectDevin(t, filepath.Join(t.TempDir(), "absent.toml"), nil, nil)
	if *calls != 0 {
		t.Fatal("a missing login must never reach the endpoint")
	}
	if pl.Source != "none" || pl.Primary != nil || pl.Secondary != nil {
		t.Fatalf("windows invented without a login: %+v", pl)
	}
	if pl.Note == nil || !strings.Contains(*pl.Note, "devin login") {
		t.Fatalf("note = %v", pl.Note)
	}
}

func TestCollectDevinLimits_RemainingInvertsToUsed(t *testing.T) {
	path := devinCredentials(t, "sk-a")
	pl, calls := collectDevin(t, path, proPlan(), nil)
	if *calls != 1 {
		t.Fatalf("fetch calls = %d", *calls)
	}
	if pl.ProviderID != "devin" || pl.Label != "Devin" {
		t.Fatalf("identity = %+v", pl)
	}
	if pl.Primary == nil || pl.Primary.UsedPercentage != 58 {
		t.Fatalf("daily = %+v", pl.Primary)
	}
	if pl.Secondary == nil || pl.Secondary.UsedPercentage != 33 {
		t.Fatalf("weekly = %+v", pl.Secondary)
	}
	if pl.Primary.WindowMinutes == nil || *pl.Primary.WindowMinutes != 1440 {
		t.Fatalf("daily window = %v", pl.Primary.WindowMinutes)
	}
	if pl.Secondary.WindowMinutes == nil || *pl.Secondary.WindowMinutes != 10080 {
		t.Fatalf("weekly window = %v", pl.Secondary.WindowMinutes)
	}
	if pl.Primary.ResetsAt == nil || *pl.Primary.ResetsAt != 1_793_520_000 {
		t.Fatalf("daily reset = %v", pl.Primary.ResetsAt)
	}
	if pl.PlanType == nil || *pl.PlanType != "Pro" {
		t.Fatalf("plan = %v", pl.PlanType)
	}
	if pl.Note == nil || !strings.Contains(*pl.Note, "-$0.32") {
		t.Fatalf("note = %v", pl.Note)
	}
}

func TestCollectDevinLimits_HiddenDailyStillSurfacesWhenItIsAllThereIs(t *testing.T) {
	status := proPlan()
	status.HideDailyQuota = true
	status.WeeklyRemainingPercent = nil
	status.WeeklyResetsAt = nil
	path := devinCredentials(t, "sk-a")
	pl, _ := collectDevin(t, path, status, nil)
	if pl.Primary != nil {
		t.Fatalf("hidden daily must not take the daily slot: %+v", pl.Primary)
	}
	if pl.Secondary == nil || pl.Secondary.UsedPercentage != 58 {
		t.Fatalf("metered daily = %+v", pl.Secondary)
	}
}

func TestCollectDevinLimits_NoQuotaIsNotAFailure(t *testing.T) {
	path := devinCredentials(t, "sk-a")
	pl, _ := collectDevin(t, path, &DevinPlanStatus{PlanName: "Free"}, nil)
	if pl.Primary != nil || pl.Secondary != nil {
		t.Fatalf("windows invented: %+v", pl)
	}
	if pl.Source != "none" || pl.Note == nil || !strings.Contains(*pl.Note, "no quota") {
		t.Fatalf("note = %v", pl.Note)
	}
}

func TestCollectDevinLimits_AFailedFetchIsReportedNotGuessed(t *testing.T) {
	path := devinCredentials(t, "sk-a")
	pl, _ := collectDevin(t, path, nil, errors.New("connection refused"))
	if pl.Primary != nil || pl.Secondary != nil {
		t.Fatalf("windows invented on failure: %+v", pl)
	}
	if pl.Note == nil || !strings.Contains(*pl.Note, "connection refused") {
		t.Fatalf("note = %v", pl.Note)
	}
}

func TestCollectDevinLimits_TheCacheServesAFreshReading(t *testing.T) {
	path := devinCredentials(t, "sk-a")
	collectDevin(t, path, proPlan(), nil)
	// The second collect shares the cache path via the same env var is not
	// possible across collectDevin's temp dir; drive the cache directly.
	cachePath := filepath.Join(t.TempDir(), "devin-quota.json")
	t.Setenv("USAGEBAR_DEVIN_CACHE_PATH", cachePath)
	calls := 0
	opts := CollectDevinLimitsOptions{
		CredentialsPath: path,
		FetchStatus: func(string, string) (*DevinPlanStatus, error) {
			calls++
			return proPlan(), nil
		},
	}
	CollectDevinLimits(nowMs, opts)
	again := CollectDevinLimits(nowMs+devinCacheSuccessTTLMs-1, opts)
	if calls != 1 {
		t.Fatalf("a fresh reading must not re-fetch, calls = %d", calls)
	}
	if again.Primary == nil || again.Primary.UsedPercentage != 58 {
		t.Fatalf("cached daily = %+v", again.Primary)
	}
}

func TestCollectDevinLimits_TheCacheNeverServesAnotherAccountsReading(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "devin-quota.json")
	t.Setenv("USAGEBAR_DEVIN_CACHE_PATH", cachePath)
	calls := 0
	opts := func(path string) CollectDevinLimitsOptions {
		return CollectDevinLimitsOptions{
			CredentialsPath: path,
			FetchStatus: func(string, string) (*DevinPlanStatus, error) {
				calls++
				return proPlan(), nil
			},
		}
	}
	accountA := devinCredentials(t, "sk-a")
	accountB := devinCredentials(t, "sk-b")
	CollectDevinLimits(nowMs, opts(accountA))
	after := CollectDevinLimits(nowMs+1, opts(accountB))
	if calls != 2 {
		t.Fatalf("a different login must not reuse the reading, calls = %d", calls)
	}
	_ = after
}

func TestCollectDevinLimits_AFailedFetchPreservesTheLastGoodReading(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "devin-quota.json")
	t.Setenv("USAGEBAR_DEVIN_CACHE_PATH", cachePath)
	path := devinCredentials(t, "sk-a")
	fails := false
	opts := CollectDevinLimitsOptions{
		CredentialsPath: path,
		FetchStatus: func(string, string) (*DevinPlanStatus, error) {
			if fails {
				return nil, errors.New("endpoint down")
			}
			return proPlan(), nil
		},
	}
	CollectDevinLimits(nowMs, opts)
	// Once the success entry expires, a failed fetch must leave the stored
	// snapshot intact for a later attempt to recover.
	fails = true
	failed := CollectDevinLimits(nowMs+devinCacheSuccessTTLMs+1, opts)
	if failed.Note == nil || !strings.Contains(*failed.Note, "endpoint down") {
		t.Fatalf("failed note = %v", failed.Note)
	}
	entry, ok := readDevinCache()
	if !ok || entry.Limits == nil || entry.Limits.Primary == nil || entry.Limits.Primary.UsedPercentage != 58 {
		t.Fatalf("last good snapshot lost: %+v", entry.Limits)
	}
	if entry.Failure == nil {
		t.Fatal("the failed attempt was not recorded")
	}
}

func TestCollectDevinLimits_TheCachedFileNeverContainsTheCredential(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "devin-quota.json")
	t.Setenv("USAGEBAR_DEVIN_CACHE_PATH", cachePath)
	path := devinCredentials(t, "sk-secret-key")
	calls := 0
	CollectDevinLimits(nowMs, CollectDevinLimitsOptions{
		CredentialsPath: path,
		FetchStatus: func(string, string) (*DevinPlanStatus, error) {
			calls++
			return proPlan(), nil
		},
	})
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-secret-key") {
		t.Fatal("the cache file must never persist the credential")
	}
}

func TestParseDevinUserStatus(t *testing.T) {
	// The shape the seat endpoint returns: proto3 JSON, int64s as strings,
	// zero-valued fields dropped.
	body := `{"userStatus":{"planStatus":{"planInfo":{"planName":"Pro"},
		"dailyQuotaRemainingPercent":42,"weeklyQuotaRemainingPercent":67,
		"dailyQuotaResetAtUnix":"1793520000","weeklyQuotaResetAtUnix":"1793520000",
		"overageBalanceMicros":"-318375"}}}`
	status, err := parseDevinUserStatus([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if status.PlanName != "Pro" {
		t.Fatalf("plan = %q", status.PlanName)
	}
	if status.DailyRemainingPercent == nil || *status.DailyRemainingPercent != 42 {
		t.Fatalf("daily = %v", status.DailyRemainingPercent)
	}
	if status.WeeklyRemainingPercent == nil || *status.WeeklyRemainingPercent != 67 {
		t.Fatalf("weekly = %v", status.WeeklyRemainingPercent)
	}
	if status.DailyResetsAt == nil || *status.DailyResetsAt != 1_793_520_000 {
		t.Fatalf("daily reset = %v", status.DailyResetsAt)
	}
	if !status.HasOverageBalance || status.OverageBalanceMicros != -318375 {
		t.Fatalf("overage = %+v", status)
	}
}

func TestParseDevinUserStatus_AbsentFieldsStayAbsent(t *testing.T) {
	// A plan with no quota windows and no overage wallet: proto3 drops every
	// zero-valued field, so nothing may be synthesized.
	status, err := parseDevinUserStatus([]byte(`{"userStatus":{"planStatus":{"planInfo":{"planName":"Free","hideDailyQuota":true}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if status.DailyRemainingPercent != nil || status.WeeklyRemainingPercent != nil ||
		status.DailyResetsAt != nil || status.WeeklyResetsAt != nil {
		t.Fatalf("absent fields synthesized: %+v", status)
	}
	if status.HasOverageBalance {
		t.Fatal("an absent wallet must not read as a zero balance")
	}
	if !status.HideDailyQuota {
		t.Fatal("hideDailyQuota dropped")
	}
}

func TestParseDevinUserStatus_RejectsNonResponses(t *testing.T) {
	for _, body := range []string{`not json`, `{"userStatus":null}`, `{}`} {
		if _, err := parseDevinUserStatus([]byte(body)); err == nil {
			t.Fatalf("body %q should be rejected", body)
		}
	}
}

func TestDevinQuotaWindow_OverConsumedClampsToFull(t *testing.T) {
	negative := -5.0
	w := devinQuotaWindow(&negative, nil, 1440)
	if w == nil || w.UsedPercentage != 100 {
		t.Fatalf("over-consumed window = %+v", w)
	}
	over := 130.0
	if w := devinQuotaWindow(&over, nil, 1440); w.UsedPercentage != 0 {
		t.Fatalf("over-provisioned window = %+v", w)
	}
	if w := devinQuotaWindow(nil, nil, 1440); w != nil {
		t.Fatal("a window with no share and no reset does not exist")
	}
}

func TestDevinAccountNote_PositiveBalanceFormats(t *testing.T) {
	status := &DevinPlanStatus{HasOverageBalance: true, OverageBalanceMicros: 1_500_000}
	note := devinAccountNote(status)
	if note == nil || *note != "extra usage balance $1.50" {
		t.Fatalf("note = %v", note)
	}
	if devinAccountNote(&DevinPlanStatus{HasOverageBalance: true}) != nil {
		t.Fatal("a zero wallet states nothing")
	}
	if devinAccountNote(&DevinPlanStatus{}) != nil {
		t.Fatal("an absent wallet states nothing")
	}
}

// The cached replay must re-stamp its fetch time: a reading served at its
// TTL edge still counts down from when it was fetched, not when it was read.
func TestCachedDevinLimits_ReplayKeepsFetchTime(t *testing.T) {
	entry := devinCacheEntry{
		FetchedAtMs: nowMs,
		AccountID:   "devin:abc",
		Outcome:     devinOutcomeFetched,
		Limits:      &ProviderLimits{ProviderID: "devin", FetchedAtMs: nowMs},
	}
	replay, ok := cachedDevinLimits(entry, "devin:abc", nowMs+devinCacheSuccessTTLMs-1)
	if !ok {
		t.Fatal("fresh entry not served")
	}
	if replay.FetchedAtMs != nowMs {
		t.Fatalf("replay fetch time = %d", replay.FetchedAtMs)
	}
	if _, ok := cachedDevinLimits(entry, "devin:other", nowMs+1); ok {
		t.Fatal("another account must not replay the entry")
	}
	if _, ok := cachedDevinLimits(entry, "devin:abc", nowMs+devinCacheSuccessTTLMs); ok {
		t.Fatal("expired entry must not replay")
	}
}

func TestCachedDevinLimits_FailedEntryReplaysFailureNotSnapshot(t *testing.T) {
	snapshot := &ProviderLimits{ProviderID: "devin", Primary: &LimitWindow{UsedPercentage: 10}}
	failure := &ProviderLimits{ProviderID: "devin", Note: strPtr("endpoint down")}
	entry := devinCacheEntry{
		FetchedAtMs: nowMs,
		AccountID:   "devin:abc",
		Outcome:     devinOutcomeFailed,
		Limits:      snapshot,
		Failure:     failure,
	}
	replay, ok := cachedDevinLimits(entry, "devin:abc", nowMs+1)
	if !ok {
		t.Fatal("fresh failed entry not served")
	}
	if replay.Primary != nil || replay.Note == nil || *replay.Note != "endpoint down" {
		t.Fatalf("replayed the preserved snapshot: %+v", replay)
	}
}

func TestDevinRoundTrip_CacheFileShape(t *testing.T) {
	// Guard the on-disk contract: anything else reading this file (or a
	// future schema change) must find the fields it expects.
	entry := devinCacheEntry{
		FetchedAtMs: nowMs, AccountID: "devin:abc", Outcome: devinOutcomeFetched,
		Limits: &ProviderLimits{ProviderID: "devin"},
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var back devinCacheEntry
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.AccountID != "devin:abc" || back.Outcome != devinOutcomeFetched || back.Limits == nil {
		t.Fatalf("round trip = %+v", back)
	}
	_ = devin.CredentialID // keep the import honest: account ids come from the provider package
}
