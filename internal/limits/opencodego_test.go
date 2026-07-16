/**
 * OpenCode Go local + web mapping tests.
 */
package limits

import (
	"encoding/json"
	"math"
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
}

func mustJSONCost(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
