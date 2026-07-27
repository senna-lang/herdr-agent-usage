/**
 * Tests for Grok auth / billing parsing.
 */
package limits

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseGrokAuthJSON_PrefersAuthXAI(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"https://accounts.x.ai/sign-in": map[string]any{
			"email": "legacy@example.com", "auth_mode": "session",
		},
		"https://auth.x.ai::abc": map[string]any{
			"email": "super@example.com", "auth_mode": "oidc", "expires_at": "2026-08-01T00:00:00Z",
		},
	})
	got := ParseGrokAuthJSON(string(raw))
	if got == nil || got.Email == nil || *got.Email != "super@example.com" {
		t.Fatalf("%+v", got)
	}
	if got.AuthMode == nil || *got.AuthMode != "oidc" {
		t.Fatalf("%+v", got)
	}
	if ParseGrokAuthJSON("not-json") != nil {
		t.Fatal("expected nil")
	}
}

func TestProviderLimitsFromBillingResult(t *testing.T) {
	email := "a@b.c"
	result := ProviderLimitsFromBillingResult(map[string]any{
		"billingCycle": map[string]any{"billingPeriodEnd": "2026-08-01T00:00:00Z"},
		"monthlyLimit": map[string]any{"val": 10000},
		"usage":        map[string]any{"totalUsed": map[string]any{"val": 2500}},
	}, 1_700_000_000_000, &email)
	if result == nil || result.Primary == nil || result.Primary.UsedPercentage != 25 {
		t.Fatalf("%+v", result)
	}
	if result.PlanType != nil {
		t.Fatal("plan should be unset")
	}
	if result.Note == nil || !containsStr(*result.Note, "$25.00") {
		t.Fatalf("note=%v", result.Note)
	}
	want := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).Unix()
	if result.Primary.ResetsAt == nil || *result.Primary.ResetsAt != want {
		t.Fatalf("resetsAt=%v", result.Primary.ResetsAt)
	}
	if ProviderLimitsFromBillingResult(map[string]any{}, 0, nil) != nil {
		t.Fatal("expected nil")
	}
}

// The xAI account OMP observed. `grok login` names the same one, when a
// usable login exists at all.
const (
	grokAccountEmail = "asrsena3302@gmail.com"
	grokAccountID    = "f3a39003-3c6f-438d-8b25-cf345f085528"
	grokNowMs        = int64(1_784_946_198_000)
	// Half an hour before now, so the borrowed note carries a stable age.
	grokObservedAtMs    = grokNowMs - 30*60_000
	grokCreditsResetMs  = grokNowMs + 3*3_600_000
	grokIncludedResetMs = grokNowMs + 20*24*3_600_000
	grokBorrowedNote    = "account " + grokAccountEmail + " · via OMP · ~30m ago"
)

// grokObservationRows is OMP's record of one xAI account's plan meters.
func grokObservationRows(email, accountID string) []usageRow {
	key := "oauth|account:" + accountID + "|email:" + email + "|org:1ee9778e-5379-4a19-a14a-2625ea6002d5"
	row := func(limitID string, fraction float64, resetsAtMs int64) usageRow {
		return usageRow{
			RecordedAtMs: grokObservedAtMs, Provider: "xai-oauth", AccountKey: key,
			Email: email, AccountID: accountID, LimitID: limitID, Label: "Grok",
			UsedFraction: fraction, Status: "ok", ResetsAtMs: resetsAtMs,
		}
	}
	return []usageRow{
		row("xai-oauth:credits:1w", 0.5, grokCreditsResetMs),
		row("xai-oauth:included:1mo", 0.25, grokIncludedResetMs),
	}
}

// grokAuthJSON is a SuperGrok entry in the shape `grok login` writes.
func grokAuthJSON(email, expiresAt string) string {
	return fmt.Sprintf(
		`{"https://auth.x.ai::supergrok":{"email":%q,"auth_mode":"oidc","key":"sk-grok-fixture","expires_at":%q}}`,
		email, expiresAt)
}

// grokDeadEnd is one path through CollectGrokLimits that yields no first-hand
// windows. Each must prefer another agent's observation of the same account,
// and keep its own honest note when there is nothing to borrow.
type grokDeadEnd struct {
	name string
	// authJSON is written to opts.AuthPath; empty leaves the file absent.
	authJSON string
	// knowsAccount is true when the collector got far enough to read the
	// login's email, which is what makes the join to an observation strict.
	knowsAccount bool
	// planTier is what FetchPlanTier answers on the paths that reach it.
	planTier   string
	wantPlan   string
	wantNote   string
	wantSource string
}

// options writes this dead end's auth.json and pins every network helper, so
// no test touches grok.com or spawns `grok agent stdio`.
func (d grokDeadEnd) options(t *testing.T) CollectGrokLimitsOptions {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	if d.authJSON != "" {
		if err := os.WriteFile(path, []byte(d.authJSON), 0o600); err != nil {
			t.Fatalf("write auth.json: %v", err)
		}
	}
	tier := d.planTier
	return CollectGrokLimitsOptions{
		AuthPath: path,
		FetchPlanTier: func(string) *string {
			if tier == "" {
				return nil
			}
			return &tier
		},
		FetchWebBilling: func(string) *LimitWindow { return nil },
		TryBillingRPC:   func(int64, *string) *ProviderLimits { return nil },
	}
}

func grokDeadEnds() []grokDeadEnd {
	future := time.UnixMilli(grokNowMs).Add(24 * time.Hour).UTC().Format(time.RFC3339)
	past := time.UnixMilli(grokNowMs).Add(-time.Hour).UTC().Format(time.RFC3339)
	const missingAuthNote = "no ~/.grok/auth.json — run `grok login`"
	return []grokDeadEnd{
		{
			name:       "auth file missing",
			wantNote:   missingAuthNote,
			wantSource: "none",
		},
		{
			name:       "auth file unparseable",
			authJSON:   "{not json",
			wantNote:   missingAuthNote,
			wantSource: "none",
		},
		{
			name:       "auth file holds no entry",
			authJSON:   "{}",
			wantNote:   missingAuthNote,
			wantSource: "none",
		},
		{
			// The real-world driver: the vendor CLI's token lapses while the
			// same subscription keeps being driven — and stays live —
			// through another harness.
			name:         "token expired",
			authJSON:     grokAuthJSON(grokAccountEmail, past),
			knowsAccount: true,
			wantNote:     "token expired (" + grokAccountEmail + ") — run `grok login`",
			wantSource:   "grok auth.json",
		},
		{
			name:         "no rate meters",
			authJSON:     grokAuthJSON(grokAccountEmail, future),
			knowsAccount: true,
			planTier:     "SUBSCRIPTION_TIER_SUPER_GROK_HEAVY",
			wantPlan:     "Heavy",
			wantNote: grokAccountEmail +
				" · rate meters unavailable (web billing failed; x.ai/billing not on agent stdio)",
			wantSource: "grok auth.json (identity only)",
		},
	}
}

func checkGrokPlan(t *testing.T, got *string, want string) {
	t.Helper()
	switch {
	case want == "" && got != nil:
		t.Fatalf("PlanType: got %q, want unset", *got)
	case want != "" && (got == nil || *got != want):
		t.Fatalf("PlanType: got %v, want %q", got, want)
	}
}

// Grok's meters need a fresh `grok login`, which a subscription driven only
// through another harness never performs. Every dead end must therefore fall
// back to that harness's observation of the same account.
func TestCollectGrokLimits_BorrowsAtEveryDeadEnd(t *testing.T) {
	for _, tc := range grokDeadEnds() {
		t.Run(tc.name, func(t *testing.T) {
			useAgentDB(t, grokObservationRows(grokAccountEmail, grokAccountID)...)
			got := CollectGrokLimits(grokNowMs, tc.options(t))

			if got.Source != "omp usage_history" {
				t.Fatalf("Source: got %q, want the borrowed observation", got.Source)
			}
			if got.ProviderID != "grok" || got.Label != "Grok" {
				t.Fatalf("borrowed row must still be Grok's, got %q/%q", got.ProviderID, got.Label)
			}
			if got.Note == nil || *got.Note != grokBorrowedNote {
				t.Fatalf("Note: got %v, want %q", got.Note, grokBorrowedNote)
			}
			if got.FetchedAtMs != grokObservedAtMs {
				t.Fatalf("FetchedAtMs: got %d, want the observation time %d", got.FetchedAtMs, grokObservedAtMs)
			}
			checkGrokPlan(t, got.PlanType, tc.wantPlan)

			if got.Primary == nil || got.Primary.UsedPercentage != 50 {
				t.Fatalf("Primary: got %+v, want 50%% used", got.Primary)
			}
			if got.Primary.WindowMinutes == nil || *got.Primary.WindowMinutes != 10080 {
				t.Fatalf("Primary window minutes: got %v, want the 1w credits meter", got.Primary.WindowMinutes)
			}
			if got.Primary.ResetsAt == nil || *got.Primary.ResetsAt != grokCreditsResetMs/1000 {
				t.Fatalf("Primary ResetsAt: got %v, want epoch seconds %d", got.Primary.ResetsAt, grokCreditsResetMs/1000)
			}
			if got.Secondary == nil || got.Secondary.UsedPercentage != 25 {
				t.Fatalf("Secondary: got %+v, want 25%% used", got.Secondary)
			}
			if got.Secondary.WindowMinutes == nil || *got.Secondary.WindowMinutes != 43200 {
				t.Fatalf("Secondary window minutes: got %v, want the 1mo included meter", got.Secondary.WindowMinutes)
			}
		})
	}
}

// The fallback must not swallow the honest message: with nothing observed,
// every dead end still says exactly why it has no numbers.
func TestCollectGrokLimits_DeadEndNotesSurviveWithoutObservation(t *testing.T) {
	for _, tc := range grokDeadEnds() {
		t.Run(tc.name, func(t *testing.T) {
			useAgentDB(t)
			got := CollectGrokLimits(grokNowMs, tc.options(t))

			if got.Note == nil || *got.Note != tc.wantNote {
				t.Fatalf("Note: got %v, want %q", got.Note, tc.wantNote)
			}
			if got.Source != tc.wantSource {
				t.Fatalf("Source: got %q, want %q", got.Source, tc.wantSource)
			}
			if got.FetchedAtMs != grokNowMs {
				t.Fatalf("FetchedAtMs: got %d, want %d", got.FetchedAtMs, grokNowMs)
			}
			checkGrokPlan(t, got.PlanType, tc.wantPlan)
			if got.Primary != nil || got.Secondary != nil || got.Tertiary != nil {
				t.Fatalf("dead end must carry no windows, got %+v", got)
			}
		})
	}
}

// An observation is the last resort, never a shortcut past grok.com's own
// answer.
func TestCollectGrokLimits_FetchedWindowOutranksObservation(t *testing.T) {
	useAgentDB(t, grokObservationRows(grokAccountEmail, grokAccountID)...)
	future := time.UnixMilli(grokNowMs).Add(24 * time.Hour).UTC().Format(time.RFC3339)
	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte(grokAuthJSON(grokAccountEmail, future)), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}

	got := CollectGrokLimits(grokNowMs, CollectGrokLimitsOptions{
		AuthPath:        authPath,
		FetchPlanTier:   func(string) *string { return nil },
		FetchWebBilling: func(string) *LimitWindow { return &LimitWindow{UsedPercentage: 7} },
		TryBillingRPC:   func(int64, *string) *ProviderLimits { return nil },
	})

	if got.Source != "grok.com GetGrokCreditsConfig" {
		t.Fatalf("Source: got %q, want the first-hand web billing source", got.Source)
	}
	if got.Primary == nil || got.Primary.UsedPercentage != 7 {
		t.Fatalf("Primary: got %+v, want the fetched 7%% window", got.Primary)
	}
	if got.Secondary != nil {
		t.Fatalf("the observation's weekly meter must not leak in: %+v", got.Secondary)
	}
	if got.FetchedAtMs != grokNowMs {
		t.Fatalf("FetchedAtMs: got %d, want the fetch time %d", got.FetchedAtMs, grokNowMs)
	}
	if want := "account " + grokAccountEmail; got.Note == nil || *got.Note != want {
		t.Fatalf("Note: got %v, want %q", got.Note, want)
	}
}

// Showing account A's numbers on a pane billing account B is worse than
// showing nothing, so a named mismatch refuses to borrow.
func TestCollectGrokLimits_RefusesAnotherAccountsObservation(t *testing.T) {
	const (
		otherEmail     = "someone-else@example.com"
		otherAccountID = "0f0c6a4e-2a51-4b1f-9f42-9c1a0d2f77aa"
	)
	for _, tc := range grokDeadEnds() {
		if !tc.knowsAccount {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			useAgentDB(t, grokObservationRows(otherEmail, otherAccountID)...)
			got := CollectGrokLimits(grokNowMs, tc.options(t))

			if got.Source == "omp usage_history" {
				t.Fatalf("borrowed %s's windows onto %s's pane", otherEmail, grokAccountEmail)
			}
			if got.Note == nil || *got.Note != tc.wantNote {
				t.Fatalf("Note: got %v, want %q", got.Note, tc.wantNote)
			}
			if got.Primary != nil || got.Secondary != nil {
				t.Fatalf("no window may come from a foreign account, got %+v", got)
			}
		})
	}
}
