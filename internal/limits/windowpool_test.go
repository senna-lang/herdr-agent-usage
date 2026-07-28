/**
 * Tests for the pure logic of the account-keyed window pool: limit-id → slot
 * routing, OMP fraction → display window conversion, folding usage_history
 * rows into per-account observations, borrow eligibility, and the rendering
 * of a borrowed row.
 *
 * Collector integration and the SQLite reader are covered elsewhere; nothing
 * here touches disk.
 */
package limits

import (
	"math"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/omp"
)

// Observed on-disk shapes. One real account reaches usage_history under
// several account_key spellings, which is exactly what the pool must collapse.
const (
	wpKeyOAuthFull   = "oauth|account:f3a39003-3c6f-438d-8b25-cf345f085528|email:asrsena3302@gmail.com|org:1ee9778e-5379-4a19-a14a-2625ea6002d5"
	wpKeyOAuthSecret = "oauth|secret:c5f777162aca2d15"
	wpKeyAPIKey      = "api_key|secret:e9554082fe7595c1"
	wpAccountID      = "f3a39003-3c6f-438d-8b25-cf345f085528"
	wpEmail          = "asrsena3302@gmail.com"
	wpResetsAtMs     = int64(1785128400228)
	wpResetsAtSec    = int64(1785128400)
)

func wpSlotName(s windowSlot) string {
	switch s {
	case slotNone:
		return "slotNone"
	case slotPrimary:
		return "slotPrimary"
	case slotSecondary:
		return "slotSecondary"
	case slotTertiary:
		return "slotTertiary"
	}
	return "windowSlot(?)"
}

// wpNearly compares display percentages. They come from float arithmetic on a
// fraction (0.28*100 is 28.000000000000004), so exact equality would assert
// the FPU rather than the contract.
func wpNearly(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func wpWindow(pct float64) *LimitWindow { return &LimitWindow{UsedPercentage: pct} }

// wpMinutes renders a *int window length for failure messages.
func wpMinutes(w *LimitWindow) string {
	if w == nil {
		return "<no window>"
	}
	if w.WindowMinutes == nil {
		return "<nil>"
	}
	return itoa(*w.WindowMinutes)
}

func TestSlotForLimitID(t *testing.T) {
	cases := []struct {
		name        string
		providerID  string
		limitID     string
		wantSlot    windowSlot
		wantMinutes int
	}{
		{"claude 5h", "claude", "anthropic:5h", slotPrimary, 300},
		{"claude 7d", "claude", "anthropic:7d", slotSecondary, 10080},
		{"claude extra has no display slot", "claude", "anthropic:extra", slotNone, 0},

		{"codex primary", "codex", "openai-codex:primary", slotPrimary, 300},
		{"codex secondary", "codex", "openai-codex:secondary", slotSecondary, 10080},

		{"grok weekly credits", "grok", "xai-oauth:credits:1w", slotPrimary, 10080},
		{"grok monthly included", "grok", "xai-oauth:included:1mo", slotSecondary, 43200},
		{"grok per-product sub-meter has no display slot", "grok", "xai-oauth:product:api:1w", slotNone, 0},

		{"opencode rolling 5h", "opencode", "rolling-5h", slotPrimary, 300},
		{"opencode weekly", "opencode", "weekly", slotSecondary, 10080},
		{"opencode monthly", "opencode", "monthly", slotTertiary, 43200},

		{"unknown id is never guessed", "claude", "anthropic:3h", slotNone, 0},
		{"empty id", "claude", "", slotNone, 0},
		{"unknown provider", "gemini", "anthropic:5h", slotNone, 0},
		{"empty provider", "", "anthropic:5h", slotNone, 0},
		{"provider id is matched exactly", "Claude", "anthropic:5h", slotNone, 0},

		// A valid id belonging to a different provider must not leak across.
		{"opencode id under claude", "claude", "rolling-5h", slotNone, 0},
		{"claude id under opencode", "opencode", "anthropic:5h", slotNone, 0},
		{"grok id under codex", "codex", "xai-oauth:credits:1w", slotNone, 0},
		{"opencode weekly under codex", "codex", "weekly", slotNone, 0},
		{"codex id under grok", "grok", "openai-codex:primary", slotNone, 0},

		// The codex ids are matched by suffix, so a longer sub-meter id that
		// merely contains "primary" is an unrecognized window, not the 5h one.
		{"codex sub-meter below the primary window", "codex", "openai-codex:primary:legacy", slotNone, 0},
		{"codex bare primary is not the suffix", "codex", "primary", slotNone, 0},
		{"codex bare secondary is not the suffix", "codex", "secondary", slotNone, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			slot, minutes := slotForLimitID(c.providerID, c.limitID)
			if slot != c.wantSlot {
				t.Fatalf("%s: slotForLimitID(%q, %q) slot = %s, want %s",
					c.name, c.providerID, c.limitID, wpSlotName(slot), wpSlotName(c.wantSlot))
			}
			if minutes != c.wantMinutes {
				t.Fatalf("%s: slotForLimitID(%q, %q) minutes = %d, want %d",
					c.name, c.providerID, c.limitID, minutes, c.wantMinutes)
			}
		})
	}
}

func TestWindowFromUsageFraction(t *testing.T) {
	// wantResetsAtSec / wantWindowMinutes of 0 mean "the pointer must be nil":
	// the production rule is that only a positive value is displayable.
	cases := []struct {
		name              string
		fraction          float64
		resetsAtMs        int64
		windowMinutes     int
		wantPct           float64
		wantResetsAtSec   int64
		wantWindowMinutes int
	}{
		{"fraction scales to percent", 0.28, wpResetsAtMs, 300, 28, wpResetsAtSec, 300},
		{"exhausted window clamps to 100", 1.0172968, wpResetsAtMs, 300, 100, wpResetsAtSec, 300},
		{"negative fraction clamps to 0", -0.5, wpResetsAtMs, 300, 0, wpResetsAtSec, 300},
		{"zero fraction", 0, wpResetsAtMs, 10080, 0, wpResetsAtSec, 10080},
		{"exactly full", 1, wpResetsAtMs, 43200, 100, wpResetsAtSec, 43200},
		{"sub-second reset truncates to the second", 0.5, 1785128400999, 300, 50, wpResetsAtSec, 300},
		{"no reset time", 0.5, 0, 300, 50, 0, 300},
		{"negative reset time is dropped", 0.5, -1, 300, 50, 0, 300},
		{"no window length", 0.5, wpResetsAtMs, 0, 50, wpResetsAtSec, 0},
		{"negative window length is dropped", 0.5, wpResetsAtMs, -1, 50, wpResetsAtSec, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := windowFromUsageFraction(c.fraction, c.resetsAtMs, c.windowMinutes)
			if w == nil {
				t.Fatalf("%s: windowFromUsageFraction returned nil", c.name)
			}
			if !wpNearly(w.UsedPercentage, c.wantPct) {
				t.Fatalf("%s: UsedPercentage = %v, want %v (fraction %v)",
					c.name, w.UsedPercentage, c.wantPct, c.fraction)
			}
			switch {
			case c.wantResetsAtSec == 0 && w.ResetsAt != nil:
				t.Fatalf("%s: ResetsAt = %d, want nil (resetsAtMs %d)", c.name, *w.ResetsAt, c.resetsAtMs)
			case c.wantResetsAtSec != 0 && w.ResetsAt == nil:
				t.Fatalf("%s: ResetsAt = nil, want %d epoch seconds (resetsAtMs %d)",
					c.name, c.wantResetsAtSec, c.resetsAtMs)
			case c.wantResetsAtSec != 0 && *w.ResetsAt != c.wantResetsAtSec:
				t.Fatalf("%s: ResetsAt = %d, want %d epoch SECONDS from %d epoch ms",
					c.name, *w.ResetsAt, c.wantResetsAtSec, c.resetsAtMs)
			}
			switch {
			case c.wantWindowMinutes == 0 && w.WindowMinutes != nil:
				t.Fatalf("%s: WindowMinutes = %d, want nil (windowMinutes %d)",
					c.name, *w.WindowMinutes, c.windowMinutes)
			case c.wantWindowMinutes != 0 && w.WindowMinutes == nil:
				t.Fatalf("%s: WindowMinutes = nil, want %d", c.name, c.wantWindowMinutes)
			case c.wantWindowMinutes != 0 && *w.WindowMinutes != c.wantWindowMinutes:
				t.Fatalf("%s: WindowMinutes = %d, want %d", c.name, *w.WindowMinutes, c.wantWindowMinutes)
			}
		})
	}
}

func TestAccountWindowsFromOMP(t *testing.T) {
	t.Run("folds an account's windows into one observation", func(t *testing.T) {
		rows := []omp.UsageWindow{
			{Provider: "anthropic", AccountKey: wpKeyOAuthFull, Email: wpEmail, AccountID: wpAccountID,
				LimitID: "anthropic:5h", UsedFraction: 0.28, ResetsAtMs: wpResetsAtMs, RecordedAtMs: 1785110000000},
			{Provider: "anthropic", AccountKey: wpKeyOAuthFull, Email: wpEmail, AccountID: wpAccountID,
				LimitID: "anthropic:7d", UsedFraction: 0.61, ResetsAtMs: 1785600000000, RecordedAtMs: 1785110000000},
		}
		got := AccountWindowsFromOMP(rows)
		if len(got) != 1 {
			t.Fatalf("fold 5h+7d: got %d observations, want 1: %+v", len(got), got)
		}
		o := got[0]
		if o.ProviderID != "claude" {
			t.Fatalf("fold 5h+7d: ProviderID = %q, want %q (collector id, not OMP's provider)", o.ProviderID, "claude")
		}
		if o.Observer != "OMP" {
			t.Fatalf("fold 5h+7d: Observer = %q, want %q", o.Observer, "OMP")
		}
		if o.Primary == nil || o.Secondary == nil {
			t.Fatalf("fold 5h+7d: want Primary and Secondary set, got Primary=%v Secondary=%v", o.Primary, o.Secondary)
		}
		if !wpNearly(o.Primary.UsedPercentage, 28) {
			t.Fatalf("fold 5h+7d: Primary.UsedPercentage = %v, want 28", o.Primary.UsedPercentage)
		}
		if !wpNearly(o.Secondary.UsedPercentage, 61) {
			t.Fatalf("fold 5h+7d: Secondary.UsedPercentage = %v, want 61", o.Secondary.UsedPercentage)
		}
		if wpMinutes(o.Primary) != "300" {
			t.Fatalf("fold 5h+7d: Primary.WindowMinutes = %s, want 300", wpMinutes(o.Primary))
		}
		if wpMinutes(o.Secondary) != "10080" {
			t.Fatalf("fold 5h+7d: Secondary.WindowMinutes = %s, want 10080", wpMinutes(o.Secondary))
		}
		if o.Tertiary != nil {
			t.Fatalf("fold 5h+7d: Tertiary = %+v, want nil (claude has no monthly window)", *o.Tertiary)
		}
		if o.ObservedAtMs != 1785110000000 {
			t.Fatalf("fold 5h+7d: ObservedAtMs = %d, want 1785110000000", o.ObservedAtMs)
		}
	})

	t.Run("uses the observed Codex duration instead of the primary ordinal", func(t *testing.T) {
		rows := []omp.UsageWindow{{
			Provider:     "openai-codex",
			AccountKey:   wpKeyOAuthFull,
			AccountID:    wpAccountID,
			LimitID:      "openai-codex:primary",
			WindowLabel:  "7 days",
			UsedFraction: 0.09,
			ResetsAtMs:   1785839498000,
			RecordedAtMs: 1785235600947,
		}}
		got := AccountWindowsFromOMP(rows)
		if len(got) != 1 {
			t.Fatalf("got %d observations, want 1: %+v", len(got), got)
		}
		if got[0].Primary == nil {
			t.Fatalf("Primary = nil, want the observed Codex window: %+v", got[0])
		}
		if wpMinutes(got[0].Primary) != "10080" {
			t.Fatalf("Primary.WindowMinutes = %s, want 10080 for %q",
				wpMinutes(got[0].Primary), rows[0].WindowLabel)
		}
		if got[0].Secondary != nil {
			t.Fatalf("Secondary = %+v, want nil for a single observed window", *got[0].Secondary)
		}
	})

	t.Run("one account under two account keys collapses", func(t *testing.T) {
		// The regression this whole pool exists for: OMP composes account_key
		// from the credential shape, so the same subscription shows up as both
		// "oauth|account:…|email:…" and "oauth|secret:…". Grouping by the raw
		// key would produce two half-populated accounts.
		identified := omp.UsageWindow{
			Provider: "anthropic", AccountKey: wpKeyOAuthFull, Email: wpEmail, AccountID: wpAccountID,
			LimitID: "anthropic:5h", UsedFraction: 0.28, ResetsAtMs: wpResetsAtMs,
		}
		opaque := omp.UsageWindow{
			Provider: "anthropic", AccountKey: wpKeyOAuthSecret, AccountID: wpAccountID,
			LimitID: "anthropic:7d", UsedFraction: 0.61, ResetsAtMs: 1785600000000,
		}
		cases := []struct {
			name             string
			identifiedAtMs   int64
			opaqueAtMs       int64
			wantObservedAtMs int64
		}{
			{"identified row recorded first", 1785110000000, 1785110500000, 1785110500000},
			{"opaque row recorded first", 1785110500000, 1785110000000, 1785110500000},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				a, b := identified, opaque
				a.RecordedAtMs = c.identifiedAtMs
				b.RecordedAtMs = c.opaqueAtMs
				got := AccountWindowsFromOMP([]omp.UsageWindow{a, b})
				if len(got) != 1 {
					t.Fatalf("%s: got %d observations, want 1 — grouping by raw account_key splits one account in two: %+v",
						c.name, len(got), got)
				}
				o := got[0]
				if o.AccountID != wpAccountID {
					t.Fatalf("%s: AccountID = %q, want %q", c.name, o.AccountID, wpAccountID)
				}
				if o.Email != wpEmail {
					t.Fatalf("%s: Email = %q, want %q — the collapsed observation must keep the identity the identified row carried",
						c.name, o.Email, wpEmail)
				}
				if o.Primary == nil || o.Secondary == nil {
					t.Fatalf("%s: want both keys' windows merged, got Primary=%v Secondary=%v", c.name, o.Primary, o.Secondary)
				}
				if o.ObservedAtMs != c.wantObservedAtMs {
					t.Fatalf("%s: ObservedAtMs = %d, want %d (newest of the merged rows)", c.name, o.ObservedAtMs, c.wantObservedAtMs)
				}
			})
		}
	})

	t.Run("email identity is case-insensitive", func(t *testing.T) {
		rows := []omp.UsageWindow{
			{Provider: "opencode-go", AccountKey: "a", Email: "User@Example.COM",
				LimitID: "rolling-5h", UsedFraction: 0.1, RecordedAtMs: 1000},
			{Provider: "opencode-go", AccountKey: "b", Email: "user@example.com",
				LimitID: "weekly", UsedFraction: 0.2, RecordedAtMs: 2000},
		}
		got := AccountWindowsFromOMP(rows)
		if len(got) != 1 {
			t.Fatalf("email case: got %d observations, want 1 (identity is the lowercased email): %+v", len(got), got)
		}
		if got[0].Primary == nil || got[0].Secondary == nil {
			t.Fatalf("email case: want both windows merged, got %+v", got[0])
		}
	})

	t.Run("routes every OMP provider to its collector", func(t *testing.T) {
		cases := []struct {
			name           string
			provider       string
			limitID        string
			wantProviderID string
			wantMinutes    string
		}{
			{"anthropic", "anthropic", "anthropic:5h", "claude", "300"},
			{"openai-codex", "openai-codex", "openai-codex:primary", "codex", "300"},
			{"xai-oauth", "xai-oauth", "xai-oauth:credits:1w", "grok", "10080"},
			{"opencode-go", "opencode-go", "rolling-5h", "opencode", "300"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got := AccountWindowsFromOMP([]omp.UsageWindow{{
					Provider: c.provider, AccountKey: wpKeyOAuthFull, Email: wpEmail, AccountID: wpAccountID,
					LimitID: c.limitID, UsedFraction: 0.5, ResetsAtMs: wpResetsAtMs, RecordedAtMs: 1000,
				}})
				if len(got) != 1 {
					t.Fatalf("%s: got %d observations, want 1: %+v", c.name, len(got), got)
				}
				if got[0].ProviderID != c.wantProviderID {
					t.Fatalf("%s: ProviderID = %q, want %q", c.name, got[0].ProviderID, c.wantProviderID)
				}
				if wpMinutes(got[0].Primary) != c.wantMinutes {
					t.Fatalf("%s: Primary.WindowMinutes = %s, want %s", c.name, wpMinutes(got[0].Primary), c.wantMinutes)
				}
			})
		}
	})

	t.Run("skips providers the route table does not route", func(t *testing.T) {
		rows := []omp.UsageWindow{
			// A limit id another provider does map: it must not sneak through
			// on an unrouted provider.
			{Provider: "deepseek", AccountKey: "dsk", Email: "a@b.com",
				LimitID: "rolling-5h", UsedFraction: 0.9, RecordedAtMs: 3000},
			// "opencode" is this plugin's collector id, not one of OMP's
			// provider ids (OMP records "opencode-go"). Treating the recorded
			// provider string as a collector id would let this row through.
			{Provider: "opencode", AccountKey: "oc", Email: "a@b.com",
				LimitID: "rolling-5h", UsedFraction: 0.8, RecordedAtMs: 3000},
			{Provider: "anthropic", AccountKey: wpKeyOAuthFull, Email: wpEmail, AccountID: wpAccountID,
				LimitID: "anthropic:5h", UsedFraction: 0.1, RecordedAtMs: 1000},
		}
		got := AccountWindowsFromOMP(rows)
		if len(got) != 1 {
			t.Fatalf("unrouted provider: got %d observations, want only the anthropic one: %+v", len(got), got)
		}
		if got[0].ProviderID != "claude" {
			t.Fatalf("unrouted provider: ProviderID = %q, want %q", got[0].ProviderID, "claude")
		}
	})

	t.Run("skips unmapped limit ids without advancing the observation time", func(t *testing.T) {
		rows := []omp.UsageWindow{
			{Provider: "anthropic", AccountKey: wpKeyOAuthFull, Email: wpEmail, AccountID: wpAccountID,
				LimitID: "anthropic:extra", UsedFraction: 0.99, ResetsAtMs: wpResetsAtMs, RecordedAtMs: 2000},
			{Provider: "anthropic", AccountKey: wpKeyOAuthFull, Email: wpEmail, AccountID: wpAccountID,
				LimitID: "anthropic:5h", UsedFraction: 0.1, ResetsAtMs: wpResetsAtMs, RecordedAtMs: 1000},
		}
		got := AccountWindowsFromOMP(rows)
		if len(got) != 1 {
			t.Fatalf("unmapped id: got %d observations, want 1: %+v", len(got), got)
		}
		o := got[0]
		if o.Primary == nil || !wpNearly(o.Primary.UsedPercentage, 10) {
			t.Fatalf("unmapped id: Primary = %+v, want the anthropic:5h window at 10%%", o.Primary)
		}
		if o.Secondary != nil || o.Tertiary != nil {
			t.Fatalf("unmapped id: anthropic:extra must not land in any slot, got Secondary=%v Tertiary=%v", o.Secondary, o.Tertiary)
		}
		if o.ObservedAtMs != 1000 {
			t.Fatalf("unmapped id: ObservedAtMs = %d, want 1000 — a skipped row must not make the observation look fresher", o.ObservedAtMs)
		}
	})

	t.Run("all rows unmapped yields no observation", func(t *testing.T) {
		rows := []omp.UsageWindow{
			{Provider: "anthropic", AccountKey: wpKeyOAuthFull, Email: wpEmail, AccountID: wpAccountID,
				LimitID: "anthropic:extra", UsedFraction: 0.99, RecordedAtMs: 2000},
			{Provider: "xai-oauth", AccountKey: "x", LimitID: "xai-oauth:product:api:1w", UsedFraction: 0.5, RecordedAtMs: 2000},
		}
		if got := AccountWindowsFromOMP(rows); len(got) != 0 {
			t.Fatalf("all rows unmapped: got %d observations, want 0: %+v", len(got), got)
		}
	})

	t.Run("no rows yields no observation", func(t *testing.T) {
		if got := AccountWindowsFromOMP(nil); len(got) != 0 {
			t.Fatalf("nil rows: got %d observations, want 0: %+v", len(got), got)
		}
	})

	t.Run("newest row wins per slot regardless of input order", func(t *testing.T) {
		older := omp.UsageWindow{
			Provider: "anthropic", AccountKey: wpKeyOAuthFull, Email: wpEmail, AccountID: wpAccountID,
			LimitID: "anthropic:5h", UsedFraction: 0.1, ResetsAtMs: 1785000000000, RecordedAtMs: 1000,
		}
		newer := omp.UsageWindow{
			Provider: "anthropic", AccountKey: wpKeyOAuthFull, Email: wpEmail, AccountID: wpAccountID,
			LimitID: "anthropic:5h", UsedFraction: 0.9, ResetsAtMs: wpResetsAtMs, RecordedAtMs: 2000,
		}
		cases := []struct {
			name string
			rows []omp.UsageWindow
		}{
			{"older first", []omp.UsageWindow{older, newer}},
			{"newer first", []omp.UsageWindow{newer, older}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got := AccountWindowsFromOMP(c.rows)
				if len(got) != 1 {
					t.Fatalf("%s: got %d observations, want 1: %+v", c.name, len(got), got)
				}
				o := got[0]
				if o.Primary == nil || !wpNearly(o.Primary.UsedPercentage, 90) {
					t.Fatalf("%s: Primary = %+v, want the recorded_at=2000 row at 90%%", c.name, o.Primary)
				}
				if o.Primary.ResetsAt == nil || *o.Primary.ResetsAt != wpResetsAtSec {
					t.Fatalf("%s: Primary.ResetsAt = %v, want %d from the newest row", c.name, o.Primary.ResetsAt, wpResetsAtSec)
				}
				if o.ObservedAtMs != 2000 {
					t.Fatalf("%s: ObservedAtMs = %d, want 2000 (the max recorded_at)", c.name, o.ObservedAtMs)
				}
			})
		}
	})

	t.Run("distinct accounts stay separate", func(t *testing.T) {
		rows := []omp.UsageWindow{
			{Provider: "anthropic", AccountKey: wpKeyOAuthFull, Email: wpEmail, AccountID: wpAccountID,
				LimitID: "anthropic:5h", UsedFraction: 0.1, RecordedAtMs: 1000},
			{Provider: "anthropic", AccountKey: "oauth|account:9999|email:other@example.com", Email: "other@example.com",
				AccountID: "9999", LimitID: "anthropic:5h", UsedFraction: 0.2, RecordedAtMs: 1000},
			// No email and no account id: the raw key is the only identity left.
			{Provider: "anthropic", AccountKey: wpKeyAPIKey,
				LimitID: "anthropic:5h", UsedFraction: 0.3, RecordedAtMs: 1000},
		}
		got := AccountWindowsFromOMP(rows)
		if len(got) != 3 {
			t.Fatalf("distinct accounts: got %d observations, want 3: %+v", len(got), got)
		}
		seen := map[string]bool{}
		for _, o := range got {
			seen[o.AccountKey] = true
		}
		for _, key := range []string{wpKeyOAuthFull, "oauth|account:9999|email:other@example.com", wpKeyAPIKey} {
			if !seen[key] {
				t.Fatalf("distinct accounts: missing an observation for account key %q, got %+v", key, got)
			}
		}
	})

	t.Run("same account key under two providers stays separate", func(t *testing.T) {
		rows := []omp.UsageWindow{
			{Provider: "anthropic", AccountKey: "shared", Email: "a@b.com",
				LimitID: "anthropic:5h", UsedFraction: 0.1, RecordedAtMs: 1000},
			{Provider: "opencode-go", AccountKey: "shared", Email: "a@b.com",
				LimitID: "rolling-5h", UsedFraction: 0.2, RecordedAtMs: 1000},
		}
		got := AccountWindowsFromOMP(rows)
		if len(got) != 2 {
			t.Fatalf("shared key across providers: got %d observations, want 2: %+v", len(got), got)
		}
	})

	t.Run("does not mutate the caller's rows", func(t *testing.T) {
		rows := []omp.UsageWindow{
			{Provider: "anthropic", AccountKey: wpKeyOAuthFull, LimitID: "anthropic:5h", RecordedAtMs: 2000},
			{Provider: "anthropic", AccountKey: wpKeyOAuthFull, LimitID: "anthropic:7d", RecordedAtMs: 1000},
		}
		AccountWindowsFromOMP(rows)
		if rows[0].LimitID != "anthropic:5h" || rows[1].LimitID != "anthropic:7d" {
			t.Fatalf("caller's rows were reordered in place: %+v", rows)
		}
	})
}

func TestSelectAccountWindows(t *testing.T) {
	// wantKey is the AccountKey of the expected observation; "" means nil.
	cases := []struct {
		name         string
		observations []AccountWindows
		providerID   string
		wantAccount  string
		wantKey      string
	}{
		{
			name: "exact email match beats a newer sibling",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-a", Email: "a@b.com", ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "k-b", Email: "c@d.com", ObservedAtMs: 900, Primary: wpWindow(20)},
			},
			providerID: "claude", wantAccount: "a@b.com", wantKey: "k-a",
		},
		{
			name: "exact account id match",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-a", AccountID: "id-a", ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "k-b", AccountID: "id-b", ObservedAtMs: 900, Primary: wpWindow(20)},
			},
			providerID: "claude", wantAccount: "id-a", wantKey: "k-a",
		},
		{
			name: "email match ignores case",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-a", Email: "a@b.com", ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "k-b", Email: "c@d.com", ObservedAtMs: 100, Primary: wpWindow(20)},
			},
			providerID: "claude", wantAccount: "A@B.CoM", wantKey: "k-a",
		},
		{
			name: "identity embedded only in the composed account key",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: wpKeyOAuthFull, ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "oauth|secret:deadbeef", ObservedAtMs: 900, Primary: wpWindow(20)},
			},
			providerID: "claude", wantAccount: wpEmail, wantKey: wpKeyOAuthFull,
		},
		{
			name: "identities exist but none match refuses",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-a", Email: "a@b.com", ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "k-b", Email: "c@d.com", ObservedAtMs: 900, Primary: wpWindow(20)},
			},
			providerID: "claude", wantAccount: "e@f.com", wantKey: "",
		},
		{
			name: "the only identified account does not match refuses",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-a", Email: "a@b.com", ObservedAtMs: 100, Primary: wpWindow(10)},
			},
			providerID: "claude", wantAccount: "e@f.com", wantKey: "",
		},
		{
			name: "one identified sibling poisons an unidentified pool",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "opaque-1", ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "k-b", Email: "c@d.com", ObservedAtMs: 900, Primary: wpWindow(20)},
			},
			providerID: "claude", wantAccount: "e@f.com", wantKey: "",
		},
		{
			name: "nobody named the account and only one was observed",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "opaque-1", ObservedAtMs: 100, Primary: wpWindow(10)},
			},
			providerID: "claude", wantAccount: "a@b.com", wantKey: "opaque-1",
		},
		{
			name: "nobody named the account and two were observed refuses",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "opaque-1", ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "opaque-2", ObservedAtMs: 900, Primary: wpWindow(20)},
			},
			providerID: "claude", wantAccount: "a@b.com", wantKey: "",
		},
		{
			name: "unnamed want with one account borrows",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "opaque-1", ObservedAtMs: 100, Primary: wpWindow(10)},
			},
			providerID: "claude", wantAccount: "", wantKey: "opaque-1",
		},
		{
			name: "unnamed want with two accounts refuses",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-a", Email: "a@b.com", ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "k-b", Email: "c@d.com", ObservedAtMs: 900, Primary: wpWindow(20)},
			},
			providerID: "claude", wantAccount: "", wantKey: "",
		},
		{
			name: "blank want is treated as unnamed",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-a", Email: "a@b.com", ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "k-b", Email: "c@d.com", ObservedAtMs: 900, Primary: wpWindow(20)},
			},
			providerID: "claude", wantAccount: "   ", wantKey: "",
		},
		{
			name: "unnamed want with one account under two keys borrows the newest",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-1", AccountID: "id-a", ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "k-2", AccountID: "id-a", ObservedAtMs: 900, Primary: wpWindow(20)},
			},
			providerID: "claude", wantAccount: "", wantKey: "k-2",
		},
		{
			name: "newest observation wins among matches",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-old", Email: "a@b.com", ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "k-new", Email: "a@b.com", ObservedAtMs: 300, Primary: wpWindow(20)},
				{ProviderID: "claude", AccountKey: "k-mid", Email: "a@b.com", ObservedAtMs: 200, Primary: wpWindow(30)},
			},
			providerID: "claude", wantAccount: "a@b.com", wantKey: "k-new",
		},
		{
			name: "windowless observations are not candidates",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-a", Email: "a@b.com", ObservedAtMs: 100, Primary: wpWindow(10)},
				{ProviderID: "claude", AccountKey: "k-empty", Email: "c@d.com", ObservedAtMs: 900},
			},
			providerID: "claude", wantAccount: "", wantKey: "k-a",
		},
		{
			name: "a windowless observation is never returned",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-empty", Email: "a@b.com", ObservedAtMs: 900},
			},
			providerID: "claude", wantAccount: "a@b.com", wantKey: "",
		},
		{
			name: "a secondary-only observation is a candidate",
			observations: []AccountWindows{
				{ProviderID: "claude", AccountKey: "k-sec", Email: "a@b.com", ObservedAtMs: 100, Secondary: wpWindow(10)},
			},
			providerID: "claude", wantAccount: "a@b.com", wantKey: "k-sec",
		},
		{
			name: "a tertiary-only observation is a candidate",
			observations: []AccountWindows{
				{ProviderID: "opencode", AccountKey: "k-ter", Email: "a@b.com", ObservedAtMs: 100, Tertiary: wpWindow(10)},
			},
			providerID: "opencode", wantAccount: "a@b.com", wantKey: "k-ter",
		},
		{
			name: "other providers are filtered out before the single-account check",
			observations: []AccountWindows{
				{ProviderID: "codex", AccountKey: "k-codex", Email: "z@z.com", ObservedAtMs: 900, Primary: wpWindow(20)},
				{ProviderID: "claude", AccountKey: "k-claude", Email: "a@b.com", ObservedAtMs: 100, Primary: wpWindow(10)},
			},
			providerID: "claude", wantAccount: "", wantKey: "k-claude",
		},
		{
			name: "a matching account under the wrong provider is refused",
			observations: []AccountWindows{
				{ProviderID: "codex", AccountKey: "k-codex", Email: "a@b.com", ObservedAtMs: 900, Primary: wpWindow(20)},
			},
			providerID: "claude", wantAccount: "a@b.com", wantKey: "",
		},
		{
			name:         "empty pool",
			observations: nil,
			providerID:   "claude", wantAccount: "a@b.com", wantKey: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SelectAccountWindows(c.observations, c.providerID, c.wantAccount)
			if c.wantKey == "" {
				if got != nil {
					t.Fatalf("%s: got observation %q, want nil (borrowing must be refused)", c.name, got.AccountKey)
				}
				return
			}
			if got == nil {
				t.Fatalf("%s: got nil, want the observation keyed %q", c.name, c.wantKey)
			}
			if got.AccountKey != c.wantKey {
				t.Fatalf("%s: got observation %q, want %q", c.name, got.AccountKey, c.wantKey)
			}
		})
	}

	t.Run("returns a copy the caller cannot use to mutate the pool", func(t *testing.T) {
		pool := []AccountWindows{
			{ProviderID: "claude", AccountKey: "k-a", Email: "a@b.com", ObservedAtMs: 100, Primary: wpWindow(10)},
		}
		got := SelectAccountWindows(pool, "claude", "a@b.com")
		if got == nil {
			t.Fatalf("copy: got nil, want the only observation")
		}
		got.Email = "mutated@example.com"
		if pool[0].Email != "a@b.com" {
			t.Fatalf("copy: mutating the result changed the pool entry to %q", pool[0].Email)
		}
	})
}

func TestBorrowedProviderLimits(t *testing.T) {
	const nowMs = int64(1785128400228)

	t.Run("note, source and timestamps", func(t *testing.T) {
		cases := []struct {
			name            string
			email           string
			observedAtMs    int64
			wantNote        string
			wantFetchedAtMs int64
		}{
			{
				name: "identified account", email: "a@b", observedAtMs: nowMs - 5*60_000,
				wantNote: "account a@b · via OMP · ~5m ago", wantFetchedAtMs: nowMs - 5*60_000,
			},
			{
				name: "unnamed account is labelled unverified", email: "", observedAtMs: nowMs - 90*60_000,
				wantNote: "account unverified · via OMP · ~1h ago", wantFetchedAtMs: nowMs - 90*60_000,
			},
			{
				name: "fresh observation", email: wpEmail, observedAtMs: nowMs - 30_000,
				wantNote: "account " + wpEmail + " · via OMP · just now", wantFetchedAtMs: nowMs - 30_000,
			},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				o := AccountWindows{
					ProviderID: "claude", AccountKey: wpKeyOAuthFull, Email: c.email,
					Primary: wpWindow(28), ObservedAtMs: c.observedAtMs, Observer: "OMP",
				}
				got := BorrowedProviderLimits(o, "claude", "Claude", nowMs)
				if got.Note == nil {
					t.Fatalf("%s: Note is nil — a borrowed number without attribution is indistinguishable from a first-hand one", c.name)
				}
				if *got.Note != c.wantNote {
					t.Fatalf("%s: Note = %q, want %q", c.name, *got.Note, c.wantNote)
				}
				if got.Source != "omp usage_history" {
					t.Fatalf("%s: Source = %q, want %q", c.name, got.Source, "omp usage_history")
				}
				if got.FetchedAtMs != c.wantFetchedAtMs {
					t.Fatalf("%s: FetchedAtMs = %d, want %d (the observation time, not now)", c.name, got.FetchedAtMs, c.wantFetchedAtMs)
				}
			})
		}
	})

	t.Run("windows and labels pass through unchanged", func(t *testing.T) {
		primary := wpWindow(28)
		secondary := wpWindow(61)
		tertiary := wpWindow(3)
		o := AccountWindows{
			ProviderID: "opencode", AccountKey: wpKeyOAuthFull, Email: wpEmail,
			Primary: primary, Secondary: secondary, Tertiary: tertiary,
			ObservedAtMs: nowMs - 60_000, Observer: "OMP",
		}
		got := BorrowedProviderLimits(o, "opencode", "OpenCode Go", nowMs)
		if got.ProviderID != "opencode" {
			t.Fatalf("pass-through: ProviderID = %q, want %q", got.ProviderID, "opencode")
		}
		if got.Label != "OpenCode Go" {
			t.Fatalf("pass-through: Label = %q, want %q", got.Label, "OpenCode Go")
		}
		if got.Primary != primary || got.Secondary != secondary || got.Tertiary != tertiary {
			t.Fatalf("pass-through: windows were rebuilt, got Primary=%v Secondary=%v Tertiary=%v",
				got.Primary, got.Secondary, got.Tertiary)
		}
	})

	t.Run("a missing window stays missing", func(t *testing.T) {
		o := AccountWindows{
			ProviderID: "claude", AccountKey: "k", Email: wpEmail,
			Primary: wpWindow(28), ObservedAtMs: nowMs - 60_000, Observer: "OMP",
		}
		got := BorrowedProviderLimits(o, "claude", "Claude", nowMs)
		if got.Secondary != nil || got.Tertiary != nil {
			t.Fatalf("missing window: got Secondary=%v Tertiary=%v, want both nil", got.Secondary, got.Tertiary)
		}
	})

	t.Run("an unset observation time falls back to now", func(t *testing.T) {
		o := AccountWindows{
			ProviderID: "claude", AccountKey: "k", Email: wpEmail,
			Primary: wpWindow(28), ObservedAtMs: 0, Observer: "OMP",
		}
		got := BorrowedProviderLimits(o, "claude", "Claude", nowMs)
		if got.FetchedAtMs != nowMs {
			t.Fatalf("unset observation time: FetchedAtMs = %d, want %d (staleness must not read as 1970)", got.FetchedAtMs, nowMs)
		}
	})
}

func TestObservationAge(t *testing.T) {
	const nowMs = int64(1785128400228)
	cases := []struct {
		name         string
		observedAtMs int64
		want         string
	}{
		{"same instant", nowMs, "just now"},
		{"just under a minute", nowMs - 59_999, "just now"},
		{"exactly a minute", nowMs - 60_000, "~1m ago"},
		{"five minutes", nowMs - 5*60_000, "~5m ago"},
		{"just under an hour", nowMs - 59*60_000, "~59m ago"},
		{"exactly an hour", nowMs - 60*60_000, "~1h ago"},
		{"two and a half hours", nowMs - 150*60_000, "~2h ago"},
		{"a day", nowMs - 24*60*60_000, "~24h ago"},
		{"clock skew into the future", nowMs + 5*60_000, "just now"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := observationAge(c.observedAtMs, nowMs); got != c.want {
				t.Fatalf("%s: observationAge(%d, %d) = %q, want %q", c.name, c.observedAtMs, nowMs, got, c.want)
			}
		})
	}
}
