/**
 * OpenCode Go local + web mapping tests.
 */
package limits

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestCostEventsFromMessageDataJSONs(t *testing.T) {
	events := CostEventsFromMessageDataJSONs([]OpenCodeTokenRow{
		{Data: mustJSONCost(map[string]any{
			"role": "assistant", "providerID": "opencode-go", "cost": 0.5,
			"time": map[string]any{"created": 1000},
		}), TimeCreated: 1000},
		{Data: mustJSONCost(map[string]any{
			"role": "assistant", "providerID": "opencode", "cost": 9,
		}), TimeCreated: 1001},
	})
	if len(events) != 1 || events[0].CostUSD != 0.5 || events[0].CreatedMs != 1000 {
		t.Fatalf("%+v", events)
	}
}

func TestStartOfUTCWeekMs(t *testing.T) {
	wed := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC).UnixMilli()
	start := StartOfUTCWeekMs(wed)
	if time.UnixMilli(start).UTC().Format(time.RFC3339) != "2026-07-13T00:00:00Z" {
		t.Fatalf("start=%s", time.UnixMilli(start).UTC())
	}
}

func TestMonthBoundsMs_Calendar(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC).UnixMilli()
	start, end := MonthBoundsMs(now, nil)
	if time.UnixMilli(start).UTC().Format(time.RFC3339) != "2026-07-01T00:00:00Z" {
		t.Fatalf("start=%s", time.UnixMilli(start).UTC())
	}
	if time.UnixMilli(end).UTC().Format(time.RFC3339) != "2026-08-01T00:00:00Z" {
		t.Fatalf("end=%s", time.UnixMilli(end).UTC())
	}
}

func TestRollingResetInSecFromEvents(t *testing.T) {
	now := int64(1_000_000_000_000)
	oldest := now - 60*60*1000
	events := []CostEvent{
		{CreatedMs: oldest, CostUSD: 1, ProviderID: "opencode-go"},
		{CreatedMs: now - 1000, CostUSD: 2, ProviderID: "opencode-go"},
	}
	sec := RollingResetInSecFromEvents(events, now, 5*3600_000)
	if sec != 4*3600 {
		t.Fatalf("sec=%d", sec)
	}
}

func TestSumCostAndProviderLimits(t *testing.T) {
	events := []CostEvent{
		{CreatedMs: 1000, CostUSD: 1},
		{CreatedMs: 2000, CostUSD: 2},
		{CreatedMs: 3000, CostUSD: 4},
	}
	if SumCostInWindow(events, 1000, 3000) != 3 {
		t.Fatal("sum")
	}

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC).UnixMilli()
	mon := time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC).UnixMilli()
	prevSun := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC).UnixMilli()
	ev := []CostEvent{
		{CreatedMs: mon, CostUSD: 15, ProviderID: "opencode-go"},
		{CreatedMs: prevSun, CostUSD: 20, ProviderID: "opencode-go"},
		{CreatedMs: now - 1000, CostUSD: 9, ProviderID: "opencode-go"},
	}
	limits := ProviderLimitsFromGoCostEvents(ev, now)
	if math.Abs(limits.Secondary.UsedPercentage-80) > 1e-5 {
		t.Fatalf("week %% = %v", limits.Secondary.UsedPercentage)
	}
	if math.Abs(limits.Primary.UsedPercentage-75) > 1e-5 {
		t.Fatalf("5h %% = %v", limits.Primary.UsedPercentage)
	}
	if limits.Tertiary == nil || limits.PlanType == nil || *limits.PlanType != "Go" {
		t.Fatalf("%+v", limits)
	}
	if limits.Note == nil || !containsStr(*limits.Note, "$30") {
		t.Fatalf("note=%v", limits.Note)
	}
}

func TestProviderLimitsFromWebSnapshot(t *testing.T) {
	now := int64(1_700_000_000_000)
	weekly := OpenCodeGoWebWindow{UsedPercentage: 17, ResetInSec: 100_000}
	monthly := OpenCodeGoWebWindow{UsedPercentage: 31, ResetInSec: 2_000_000}
	limits := ProviderLimitsFromWebSnapshot(OpenCodeGoWebSnapshot{
		Rolling: OpenCodeGoWebWindow{UsedPercentage: 42, ResetInSec: 1800},
		Weekly:  &weekly, Monthly: &monthly,
		WorkspaceID: "wrk_test", Source: "web",
	}, now)
	if limits.Primary == nil || limits.Primary.UsedPercentage != 42 {
		t.Fatalf("%+v", limits.Primary)
	}
	if limits.Primary.ResetsAt == nil || *limits.Primary.ResetsAt != now/1000+1800 {
		t.Fatalf("resetsAt=%v", limits.Primary.ResetsAt)
	}
	if limits.Secondary == nil || limits.Secondary.UsedPercentage != 17 {
		t.Fatalf("%+v", limits.Secondary)
	}
	if limits.Tertiary == nil || limits.Tertiary.UsedPercentage != 31 {
		t.Fatalf("%+v", limits.Tertiary)
	}
	if limits.Source != "web" {
		t.Fatalf("source=%q", limits.Source)
	}
	// Official numbers carry no note: the workspace id it used to show meant
	// nothing to a reader, and an empty note is what distinguishes these from
	// the local path's "est. spent …" line.
	if limits.Note != nil {
		t.Fatalf("web snapshot should carry no note, got %q", *limits.Note)
	}
}

func mustJSONCost(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

const (
	openCodeNowMs = int64(1_784_946_198_000)
	// Three quarters of an hour before now, for a stable borrowed age.
	openCodeObservedAtMs = openCodeNowMs - 45*60_000
	openCodeEmail        = "asrsena3302@gmail.com"
	openCodeAccountID    = "f3a39003-3c6f-438d-8b25-cf345f085528"
	openCodeRollingReset = openCodeNowMs + 2*3_600_000
	openCodeWeeklyReset  = openCodeNowMs + 3*24*3_600_000
	openCodeMonthlyReset = openCodeNowMs + 12*24*3_600_000
	// One assistant message an hour ago: $4.50 of the $12 five-hour cap.
	openCodeLocalCostUSD  = 4.5
	openCodeLocalUsedPct  = 37.5
	openCodeLocalSource   = "opencode.db local cost (Go caps, calendar week/month)"
	openCodeBorrowedNote  = "account " + openCodeEmail + " · via OMP · ~45m ago"
	openCodeUnverifiedNot = "account unverified · via OMP · ~45m ago"
)

// neutralizeOpenCodeWeb keeps opencode.ai out of the collector: a private
// cache file, no pasted cookie header, and no browser session import. What
// remains is the borrow-vs-local-estimate decision under test.
func neutralizeOpenCodeWeb(t *testing.T) {
	t.Helper()
	t.Setenv("USAGEBAR_OPENCODE_WEB_CACHE_PATH", filepath.Join(t.TempDir(), "opencode-go-web.json"))
	t.Setenv("USAGEBAR_DISABLE_BROWSER_COOKIES", "1")
	t.Setenv("OPENCODE_GO_COOKIE", "")
}

// openCodeObservationRows is OMP's record of one opencode-go account's three
// official windows.
func openCodeObservationRows(accountKey, email, accountID string) []usageRow {
	row := func(limitID string, fraction float64, resetsAtMs int64) usageRow {
		return usageRow{
			RecordedAtMs: openCodeObservedAtMs, Provider: "opencode-go", AccountKey: accountKey,
			Email: email, AccountID: accountID, LimitID: limitID, Label: "OpenCode",
			UsedFraction: fraction, Status: "ok", ResetsAtMs: resetsAtMs,
		}
	}
	return []usageRow{
		row("rolling-5h", 0.5, openCodeRollingReset),
		row("weekly", 0.25, openCodeWeeklyReset),
		row("monthly", 0.125, openCodeMonthlyReset),
	}
}

// openCodeOAuthRows is the identified account, keyed the way OMP composes an
// OAuth key.
func openCodeOAuthRows() []usageRow {
	key := "oauth|account:" + openCodeAccountID + "|email:" + openCodeEmail +
		"|org:1ee9778e-5379-4a19-a14a-2625ea6002d5"
	return openCodeObservationRows(key, openCodeEmail, openCodeAccountID)
}

// writeOpenCodeCostDB builds a minimal opencode.db holding one assistant
// message per cost, an hour before openCodeNowMs.
func writeOpenCodeCostDB(t *testing.T, providerID string, costs ...float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture opencode.db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE message (
		id TEXT PRIMARY KEY, data TEXT, time_created INTEGER)`); err != nil {
		t.Fatalf("create fixture message table: %v", err)
	}
	created := openCodeNowMs - 3_600_000
	for i, cost := range costs {
		data, err := json.Marshal(map[string]any{
			"role":       "assistant",
			"providerID": providerID,
			"cost":       cost,
			"time":       map[string]any{"created": created},
		})
		if err != nil {
			t.Fatalf("marshal fixture message: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO message (id, data, time_created) VALUES (?,?,?)`,
			fmt.Sprintf("msg-%d", i), string(data), created); err != nil {
			t.Fatalf("insert fixture message: %v", err)
		}
	}
	return path
}

func checkOpenCodeWindow(t *testing.T, name string, got *LimitWindow, wantUsed float64, wantMinutes int, wantResetsAtMs int64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: missing window", name)
	}
	if got.UsedPercentage != wantUsed {
		t.Fatalf("%s used: got %v, want %v", name, got.UsedPercentage, wantUsed)
	}
	if got.WindowMinutes == nil || *got.WindowMinutes != wantMinutes {
		t.Fatalf("%s window minutes: got %v, want %d", name, got.WindowMinutes, wantMinutes)
	}
	if got.ResetsAt == nil || *got.ResetsAt != wantResetsAtMs/1000 {
		t.Fatalf("%s ResetsAt: got %v, want epoch seconds %d", name, got.ResetsAt, wantResetsAtMs/1000)
	}
}

// opencode-go keeps no usage numbers on disk, so without a web session
// another agent's observation is the only real server data available.
func TestCollectOpenCodeLimits_BorrowsWhenWebUnavailable(t *testing.T) {
	neutralizeOpenCodeWeb(t)
	useAgentDB(t, openCodeOAuthRows()...)

	got := CollectOpenCodeLimits(openCodeNowMs, filepath.Join(t.TempDir(), "absent.db"))

	if got.Source != "omp usage_history" {
		t.Fatalf("Source: got %q, want the borrowed observation", got.Source)
	}
	if got.ProviderID != "opencode" || got.Label != "OpenCode" {
		t.Fatalf("borrowed row must still be OpenCode's, got %q/%q", got.ProviderID, got.Label)
	}
	if got.Note == nil || *got.Note != openCodeBorrowedNote {
		t.Fatalf("Note: got %v, want %q", got.Note, openCodeBorrowedNote)
	}
	if got.FetchedAtMs != openCodeObservedAtMs {
		t.Fatalf("FetchedAtMs: got %d, want the observation time %d", got.FetchedAtMs, openCodeObservedAtMs)
	}
	checkOpenCodeWindow(t, "Primary", got.Primary, 50, 300, openCodeRollingReset)
	checkOpenCodeWindow(t, "Secondary", got.Secondary, 25, 10080, openCodeWeeklyReset)
	checkOpenCodeWindow(t, "Tertiary", got.Tertiary, 12.5, 43200, openCodeMonthlyReset)
}

// The precedence that matters. The local DB is list-price arithmetic over
// message costs; an observation is what the server actually reported. With
// both on disk the observation must win, or correct numbers silently
// downgrade to an estimate. The second case proves the local DB really would
// have answered — and answered differently — so the first is not vacuous.
func TestCollectOpenCodeLimits_ObservationOutranksLocalEstimate(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rows       []usageRow
		wantSource string
		wantUsed   float64
	}{
		{"observed", openCodeOAuthRows(), "omp usage_history", 50},
		{"not observed", nil, openCodeLocalSource, openCodeLocalUsedPct},
	} {
		t.Run(tc.name, func(t *testing.T) {
			neutralizeOpenCodeWeb(t)
			useAgentDB(t, tc.rows...)
			dbPath := writeOpenCodeCostDB(t, "opencode-go", openCodeLocalCostUSD)

			got := CollectOpenCodeLimits(openCodeNowMs, dbPath)

			if got.Source != tc.wantSource {
				t.Fatalf("Source: got %q, want %q", got.Source, tc.wantSource)
			}
			if got.Primary == nil || got.Primary.UsedPercentage != tc.wantUsed {
				t.Fatalf("Primary: got %+v, want %v%% used", got.Primary, tc.wantUsed)
			}
		})
	}
}

// With nothing observed the collector keeps its pre-existing local behaviour.
func TestCollectOpenCodeLimits_LocalFallbackWithoutObservation(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.db")
	for _, tc := range []struct {
		name       string
		dbPath     func(t *testing.T) string
		wantSource func(dbPath string) string
		wantNote   func(dbPath string) string
	}{
		{
			name:       "db missing",
			dbPath:     func(*testing.T) string { return missing },
			wantSource: func(string) string { return "none" },
			wantNote:   func(p string) string { return "db not found: " + p },
		},
		{
			name: "db holds no opencode-go costs",
			dbPath: func(t *testing.T) string {
				return writeOpenCodeCostDB(t, "deepseek", openCodeLocalCostUSD)
			},
			wantSource: func(p string) string { return p },
			wantNote:   func(string) string { return "no opencode-go assistant costs in local db" },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			neutralizeOpenCodeWeb(t)
			useAgentDB(t)
			dbPath := tc.dbPath(t)

			got := CollectOpenCodeLimits(openCodeNowMs, dbPath)

			if got.Source != tc.wantSource(dbPath) {
				t.Fatalf("Source: got %q, want %q", got.Source, tc.wantSource(dbPath))
			}
			if got.Note == nil || *got.Note != tc.wantNote(dbPath) {
				t.Fatalf("Note: got %v, want %q", got.Note, tc.wantNote(dbPath))
			}
			if got.Primary != nil || got.Secondary != nil || got.Tertiary != nil {
				t.Fatalf("no windows may be invented, got %+v", got)
			}
			if got.FetchedAtMs != openCodeNowMs {
				t.Fatalf("FetchedAtMs: got %d, want %d", got.FetchedAtMs, openCodeNowMs)
			}
		})
	}
}

// OMP records some opencode-go accounts by opaque key alone. A single such
// account is still borrowable — labelled unverified, never as a confirmed
// identity.
func TestCollectOpenCodeLimits_BorrowsUnverifiedAccount(t *testing.T) {
	neutralizeOpenCodeWeb(t)
	useAgentDB(t, openCodeObservationRows("api_key|secret:e9554082fe7595c1", "", "")...)

	got := CollectOpenCodeLimits(openCodeNowMs, filepath.Join(t.TempDir(), "absent.db"))

	if got.Source != "omp usage_history" {
		t.Fatalf("Source: got %q, want the borrowed observation", got.Source)
	}
	if got.Note == nil || *got.Note != openCodeUnverifiedNot {
		t.Fatalf("Note: got %v, want %q", got.Note, openCodeUnverifiedNot)
	}
	checkOpenCodeWindow(t, "Primary", got.Primary, 50, 300, openCodeRollingReset)
	checkOpenCodeWindow(t, "Tertiary", got.Tertiary, 12.5, 43200, openCodeMonthlyReset)
}
