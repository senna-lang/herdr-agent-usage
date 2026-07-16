/**
 * OpenCode Go limits collection (local cost pure helpers + DB read).
 * Web cookie fetch is optional and skipped when cookie is unset.
 */
package limits

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/planlabels"
	_ "modernc.org/sqlite"
)

// OpenCodeGoCaps are official USD caps.
var OpenCodeGoCaps = struct {
	FiveHourUsd float64
	WeeklyUsd   float64
	MonthlyUsd  float64
}{12, 30, 60}

const (
	fiveHourMs = 5 * 60 * 60 * 1000
	weekMs     = 7 * 24 * 60 * 60 * 1000
)

// CostEvent is one opencode-go assistant cost observation.
type CostEvent struct {
	CreatedMs  int64
	CostUSD    float64
	ProviderID string
}

// OpenCodeGoWebWindow is a web usage window.
type OpenCodeGoWebWindow struct {
	UsedPercentage float64
	ResetInSec     int
}

// OpenCodeGoWebSnapshot is official web usage.
type OpenCodeGoWebSnapshot struct {
	Rolling     OpenCodeGoWebWindow
	Weekly      *OpenCodeGoWebWindow
	Monthly     *OpenCodeGoWebWindow
	WorkspaceID string
	Source      string
}

// CostEventsFromMessageDataJSONs extracts opencode-go assistant costs (pure).
func CostEventsFromMessageDataJSONs(rows []OpenCodeTokenRow) []CostEvent {
	var out []CostEvent
	for _, row := range rows {
		var parsed struct {
			Role       string   `json:"role"`
			ProviderID string   `json:"providerID"`
			Cost       *float64 `json:"cost"`
			Time       *struct {
				Created *float64 `json:"created"`
			} `json:"time"`
		}
		if err := json.Unmarshal([]byte(row.Data), &parsed); err != nil {
			continue
		}
		if parsed.Role != "assistant" || parsed.ProviderID != "opencode-go" {
			continue
		}
		if parsed.Cost == nil || !isFiniteF(*parsed.Cost) || *parsed.Cost < 0 {
			continue
		}
		created := row.TimeCreated
		if parsed.Time != nil && parsed.Time.Created != nil && isFiniteF(*parsed.Time.Created) {
			created = int64(*parsed.Time.Created)
		}
		out = append(out, CostEvent{CreatedMs: created, CostUSD: *parsed.Cost, ProviderID: "opencode-go"})
	}
	return out
}

// SumCostInWindow sums costs in half-open [startMs, endMs).
func SumCostInWindow(events []CostEvent, startMs, endMs int64) float64 {
	var sum float64
	for _, e := range events {
		if e.CreatedMs >= startMs && e.CreatedMs < endMs {
			sum += e.CostUSD
		}
	}
	return sum
}

// StartOfUTCWeekMs returns Monday 00:00 UTC of the week containing nowMs.
func StartOfUTCWeekMs(nowMs int64) int64 {
	now := time.UnixMilli(nowMs).UTC()
	day := int(now.Weekday()) // 0=Sun
	daysFromMonday := (day + 6) % 7
	monday := time.Date(now.Year(), now.Month(), now.Day()-daysFromMonday, 0, 0, 0, 0, time.UTC)
	return monday.UnixMilli()
}

// MonthBoundsMs returns monthly period bounds (anchor = oldest event or calendar month).
func MonthBoundsMs(nowMs int64, earliestCreatedMs *int64) (startMs, endMs int64) {
	if earliestCreatedMs == nil {
		now := time.UnixMilli(nowMs).UTC()
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		return start.UnixMilli(), end.UnixMilli()
	}
	anchor := time.UnixMilli(*earliestCreatedMs).UTC()
	aDay, aH, aM, aS, aMs := anchor.Day(), anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond()/1e6
	now := time.UnixMilli(nowMs).UTC()
	y, m := now.Year(), now.Month()
	makeAt := func(year int, month time.Month) int64 {
		lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
		day := aDay
		if day > lastDay {
			day = lastDay
		}
		return time.Date(year, month, day, aH, aM, aS, aMs*1e6, time.UTC).UnixMilli()
	}
	start := makeAt(y, m)
	if start > nowMs {
		m--
		if m < 1 {
			m = 12
			y--
		}
		start = makeAt(y, m)
	}
	endM := m + 1
	endY := y
	if endM > 12 {
		endM = 1
		endY++
	}
	end := makeAt(endY, endM)
	return start, end
}

// RollingResetInSecFromEvents returns seconds until oldest in-window cost expires.
func RollingResetInSecFromEvents(events []CostEvent, nowMs, windowMs int64) int64 {
	start := nowMs - windowMs
	var oldest *int64
	for _, e := range events {
		if e.CreatedMs >= start && e.CreatedMs < nowMs {
			if oldest == nil || e.CreatedMs < *oldest {
				v := e.CreatedMs
				oldest = &v
			}
		}
	}
	base := nowMs
	if oldest != nil {
		base = *oldest
	}
	sec := (base + windowMs - nowMs) / 1000
	if sec < 0 {
		return 0
	}
	return sec
}

// EstimateRollingResetsAtEpochSeconds returns reset epoch when window has events.
func EstimateRollingResetsAtEpochSeconds(events []CostEvent, nowMs, windowMs int64) *int64 {
	start := nowMs - windowMs
	has := false
	for _, e := range events {
		if e.CreatedMs >= start && e.CreatedMs <= nowMs {
			has = true
			break
		}
	}
	if !has {
		return nil
	}
	resetIn := RollingResetInSecFromEvents(events, nowMs, windowMs)
	v := nowMs/1000 + resetIn
	return &v
}

func percentUsed(spentUSD, capUSD float64) float64 {
	if !isFiniteF(spentUSD) || capUSD <= 0 {
		return 0
	}
	value := math.Max(0, math.Min(100, (spentUSD/capUSD)*100))
	return math.Round(value*10) / 10
}

func windowFromUsed(usedPercentage float64, windowMinutes int, resetsAt *int64) *LimitWindow {
	return &LimitWindow{
		UsedPercentage: usedPercentage,
		WindowMinutes:  &windowMinutes,
		ResetsAt:       resetsAt,
	}
}

func resetsAtFromResetInSec(nowMs int64, resetInSec int64) *int64 {
	if resetInSec < 0 {
		resetInSec = 0
	}
	v := nowMs/1000 + resetInSec
	return &v
}

// ProviderLimitsFromGoCostEvents builds limits from local cost events.
func ProviderLimitsFromGoCostEvents(events []CostEvent, nowMs int64) ProviderLimits {
	fiveStart := nowMs - fiveHourMs
	weekStart := StartOfUTCWeekMs(nowMs)
	weekEnd := weekStart + weekMs
	var earliest *int64
	if len(events) > 0 {
		min := events[0].CreatedMs
		for _, e := range events {
			if e.CreatedMs < min {
				min = e.CreatedMs
			}
		}
		earliest = &min
	}
	monthStart, monthEnd := MonthBoundsMs(nowMs, earliest)

	fiveH := SumCostInWindow(events, fiveStart, nowMs)
	week := SumCostInWindow(events, weekStart, minI64(weekEnd, nowMs+1))
	monthSpend := SumCostInWindow(events, monthStart, minI64(monthEnd, nowMs+1))

	fiveResetIn := RollingResetInSecFromEvents(events, nowMs, fiveHourMs)
	weekResetIn := maxI64(0, (weekEnd-nowMs)/1000)
	monthResetIn := maxI64(0, (monthEnd-nowMs)/1000)

	plan := planlabels.OpencodePlanLabel(strPtr("go"))
	note := fmt.Sprintf(
		"est. spent 5h=$%.2f/$%.0f · week=$%.2f/$%.0f · month=$%.2f/$%.0f",
		fiveH, OpenCodeGoCaps.FiveHourUsd, week, OpenCodeGoCaps.WeeklyUsd, monthSpend, OpenCodeGoCaps.MonthlyUsd,
	)
	return ProviderLimits{
		ProviderID:  "opencode",
		Label:       "OpenCode",
		PlanType:    plan,
		Primary:     windowFromUsed(percentUsed(fiveH, OpenCodeGoCaps.FiveHourUsd), 300, resetsAtFromResetInSec(nowMs, fiveResetIn)),
		Secondary:   windowFromUsed(percentUsed(week, OpenCodeGoCaps.WeeklyUsd), 10080, resetsAtFromResetInSec(nowMs, weekResetIn)),
		Tertiary:    windowFromUsed(percentUsed(monthSpend, OpenCodeGoCaps.MonthlyUsd), 30*24*60, resetsAtFromResetInSec(nowMs, monthResetIn)),
		Source:      "opencode.db local cost (Go caps, calendar week/month)",
		FetchedAtMs: nowMs,
		Note:        &note,
	}
}

// ProviderLimitsFromWebSnapshot maps web usagePercent to used% + resetsAt.
func ProviderLimitsFromWebSnapshot(snap OpenCodeGoWebSnapshot, nowMs int64) ProviderLimits {
	plan := planlabels.OpencodePlanLabel(strPtr("go"))
	note := "workspace " + snap.WorkspaceID
	var weekly, monthly *LimitWindow
	if snap.Weekly != nil {
		weekly = windowFromUsed(snap.Weekly.UsedPercentage, 10080, resetsAtFromResetInSec(nowMs, int64(snap.Weekly.ResetInSec)))
	}
	if snap.Monthly != nil {
		monthly = windowFromUsed(snap.Monthly.UsedPercentage, 30*24*60, resetsAtFromResetInSec(nowMs, int64(snap.Monthly.ResetInSec)))
	}
	return ProviderLimits{
		ProviderID:  "opencode",
		Label:       "OpenCode",
		PlanType:    plan,
		Primary:     windowFromUsed(snap.Rolling.UsedPercentage, 300, resetsAtFromResetInSec(nowMs, int64(snap.Rolling.ResetInSec))),
		Secondary:   weekly,
		Tertiary:    monthly,
		Source:      snap.Source,
		FetchedAtMs: nowMs,
		Note:        &note,
	}
}

func minI64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxI64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ResolveOpenCodeLimitsDBPath resolves the OpenCode sqlite path for limits
// (existence-checked candidates when OPENCODE_DATA_DIR is set).
func ResolveOpenCodeLimitsDBPath() string {
	if v := os.Getenv("OPENCODE_DB"); v != "" {
		return v
	}
	// OPENCODE_DATA_DIR may be either a data root (…/share) or already the opencode dir.
	if dataDir := os.Getenv("OPENCODE_DATA_DIR"); dataDir != "" {
		candidates := []string{
			filepath.Join(dataDir, "opencode.db"),
			filepath.Join(dataDir, "opencode", "opencode.db"),
		}
		for _, p := range candidates {
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
				return p
			}
		}
		return candidates[0]
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode", "opencode.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

func loadCostEventsFromDB(dbPath string) ([]CostEvent, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Prefer the richer message+part query when the `part` table exists.
	var hasPart int
	_ = db.QueryRow(`SELECT 1 FROM sqlite_master WHERE type='table' AND name='part' LIMIT 1`).Scan(&hasPart)
	if hasPart == 1 {
		rows, err := db.Query(`
			WITH message_costs AS (
			  SELECT
			    id AS messageID,
			    CAST(COALESCE(json_extract(data, '$.time.created'), time_created) AS INTEGER) AS timeCreated,
			    CAST(json_extract(data, '$.cost') AS REAL) AS cost
			  FROM message
			  WHERE json_valid(data)
			    AND json_extract(data, '$.providerID') = 'opencode-go'
			    AND json_extract(data, '$.role') = 'assistant'
			    AND json_type(data, '$.cost') IN ('integer', 'real')
			)
			SELECT timeCreated, cost FROM message_costs
			UNION ALL
			SELECT
			  CAST(COALESCE(json_extract(p.data, '$.time.created'), p.time_created, m.time_created) AS INTEGER),
			  CAST(json_extract(p.data, '$.cost') AS REAL)
			FROM part p
			JOIN message m ON m.id = p.message_id
			WHERE json_valid(p.data)
			  AND json_valid(m.data)
			  AND json_extract(p.data, '$.type') = 'step-finish'
			  AND json_type(p.data, '$.cost') IN ('integer', 'real')
			  AND json_extract(m.data, '$.providerID') = 'opencode-go'
			  AND json_extract(m.data, '$.role') = 'assistant'
			  AND NOT EXISTS (
			    SELECT 1 FROM message_costs WHERE message_costs.messageID = p.message_id
			  )`)
		if err == nil {
			defer rows.Close()
			var out []CostEvent
			for rows.Next() {
				var tc int64
				var cost float64
				if err := rows.Scan(&tc, &cost); err != nil {
					continue
				}
				if !isFiniteF(cost) || cost < 0 {
					continue
				}
				out = append(out, CostEvent{CreatedMs: tc, CostUSD: cost, ProviderID: "opencode-go"})
			}
			return out, nil
		}
	}

	rows, err := db.Query(`
		SELECT data, CAST(COALESCE(json_extract(data, '$.time.created'), time_created) AS INTEGER) AS timeCreated
		FROM message
		WHERE json_valid(data)
		  AND json_extract(data, '$.providerID') = 'opencode-go'
		  AND json_extract(data, '$.role') = 'assistant'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []OpenCodeTokenRow
	for rows.Next() {
		var data string
		var tc int64
		if err := rows.Scan(&data, &tc); err != nil {
			continue
		}
		list = append(list, OpenCodeTokenRow{Data: data, TimeCreated: tc})
	}
	return CostEventsFromMessageDataJSONs(list), nil
}

// CollectOpenCodeLimits prefers web (OPENCODE_GO_COOKIE) then local DB.
func CollectOpenCodeLimits(nowMs int64, dbPath string) ProviderLimits {
	if web := FetchOpenCodeGoWebUsage("", nowMs); web != nil {
		return ProviderLimitsFromWebSnapshot(*web, nowMs)
	}
	if dbPath == "" {
		dbPath = ResolveOpenCodeLimitsDBPath()
	}
	if _, err := os.Stat(dbPath); err != nil {
		note := "db not found: " + dbPath
		return ProviderLimits{ProviderID: "opencode", Label: "OpenCode", Source: "none", FetchedAtMs: nowMs, Note: &note}
	}
	events, err := loadCostEventsFromDB(dbPath)
	if err != nil {
		note := "db read failed: " + err.Error()
		return ProviderLimits{ProviderID: "opencode", Label: "OpenCode", Source: dbPath, FetchedAtMs: nowMs, Note: &note}
	}
	if len(events) == 0 {
		note := "no opencode-go assistant costs in local db"
		return ProviderLimits{ProviderID: "opencode", Label: "OpenCode", Source: dbPath, FetchedAtMs: nowMs, Note: &note}
	}
	return ProviderLimitsFromGoCostEvents(events, nowMs)
}
