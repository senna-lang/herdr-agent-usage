package limits

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

func int64Ptr(n int64) *int64 { return &n }

// The v1 document names its fields explicitly. Renaming one is a breaking
// change for every host that renders it, so the key sets are pinned here.
func TestBuildCollectReport_PinsTheV1FieldNames(t *testing.T) {
	plan := "Max"
	nowMs := int64(1_000_000_000_000)
	report := BuildCollectReport(CollectReportInput{
		Providers: []ProviderLimits{{
			ProviderID: "claude", Label: "personal", PlanType: &plan,
			Primary:      &LimitWindow{UsedPercentage: 38, ResetsAt: int64Ptr(nowMs/1000 + 2*3600 + 14*60), WindowMinutes: intPtr(300), RunOut: &RunOutEstimate{MinutesToEmpty: 38, EmptyBeforeReset: true}},
			PaneActivity: &ProviderPaneActivity{WindowMinutes: 300, TotalTokens: 10, Panes: []PaneActivityShare{{PaneID: "p1", Label: "api", Tokens: 10, SharePercent: 100}}},
		}},
		APIUsage: []APIProviderUsage{{BackendID: "deepseek", Label: "DeepSeek", Windows: []APIUsageWindow{{WindowMinutes: 1440, Tokens: 5}}, Models: []APIModelUsage{{ModelID: "m"}}}},
		Filter:   CollectFilterAll,
		NowMs:    nowMs,
	})
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	keysOf := func(v any) []string {
		m := v.(map[string]any)
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	provider := doc["providers"].([]any)[0].(map[string]any)
	window := provider["windows"].([]any)[0].(map[string]any)
	api := doc["apiUsage"].([]any)[0].(map[string]any)
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"document", keysOf(doc), []string{"apiUsage", "emptyMessage", "filter", "generatedAtMs", "limitPercent", "lowCachePanes", "providers", "version"}},
		{"provider", keysOf(provider), []string{"accountLabel", "action", "fetchedAtMs", "groupLabel", "identity", "label", "note", "paneActivity", "planType", "profileId", "profileLabel", "providerId", "source", "staleMinutes", "status", "windows"}},
		{"window", keysOf(window), []string{"band", "barFill", "displayPercent", "remainingPercent", "resetIn", "resetsAt", "runOut", "slot", "tag", "usedPercentage", "windowMinutes"}},
		{"runOut", keysOf(window["runOut"]), []string{"emptyBeforeReset", "minutesToEmpty", "text"}},
		{"paneActivity", keysOf(provider["paneActivity"]), []string{"panes", "tag", "totalTokens", "windowMinutes"}},
		{"apiUsage", keysOf(api), []string{"backendId", "hasCost", "label", "models", "paneActivity", "windows"}},
	}
	for _, tc := range cases {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s keys = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	if doc["version"].(float64) != CollectReportVersion {
		t.Fatalf("version = %v", doc["version"])
	}
}

// Derived values use the text panel's own helpers, so the app and the TUI
// print the same words for the same reading.
func TestBuildCollectReport_DerivesTheTextPanelsWording(t *testing.T) {
	nowMs := int64(1_000_000_000_000)
	stale := 42
	report := BuildCollectReport(CollectReportInput{
		Providers: []ProviderLimits{{
			ProviderID: "codex",
			Primary:    &LimitWindow{UsedPercentage: 91, ResetsAt: int64Ptr(nowMs/1000 + 44*60), WindowMinutes: intPtr(300), RunOut: &RunOutEstimate{MinutesToEmpty: 31, EmptyBeforeReset: true}},
			Secondary:  &LimitWindow{UsedPercentage: 56, WindowMinutes: intPtr(10080)},
			Tertiary:   &LimitWindow{UsedPercentage: 20},
		}, {
			ProviderID: "codex-dev", Secondary: &LimitWindow{UsedPercentage: 27}, StaleMinutes: &stale,
		}, {
			ProviderID: "grok", Action: "grok login",
		}},
		Filter:       CollectFilterOpenPanes,
		LimitPercent: core.LimitPercentUsed,
		NowMs:        nowMs,
	})
	if report.EmptyMessage != "(no agent panes open)" {
		t.Fatalf("EmptyMessage = %q", report.EmptyMessage)
	}
	codex := report.Providers[0]
	if codex.Status != StatusOK || len(codex.Windows) != 3 {
		t.Fatalf("codex = %+v", codex)
	}
	w := codex.Windows[0]
	if w.Slot != "primary" || w.Tag != "5h" || w.RemainingPercent != 9 || w.DisplayPercent != 91 || w.BarFill != 0.91 || w.Band != "low" || w.ResetIn != "44m" {
		t.Fatalf("primary window = %+v", w)
	}
	if w.RunOut == nil || w.RunOut.Text != "empty in ~31m" {
		t.Fatalf("runOut = %+v", w.RunOut)
	}
	if got := codex.Windows[1]; got.Tag != "7d" || got.Band != "mid" || got.ResetIn != "" || got.RunOut != nil {
		t.Fatalf("secondary window = %+v", got)
	}
	// A window without WindowMinutes falls back to its slot's tag.
	if got := codex.Windows[2]; got.Slot != "tertiary" || got.Tag != "30d" || got.Band != "high" {
		t.Fatalf("tertiary window = %+v", got)
	}
	if dev := report.Providers[1]; dev.Status != StatusStale || *dev.StaleMinutes != 42 || dev.Windows[0].Slot != "secondary" {
		t.Fatalf("dev = %+v", dev)
	}
	if grok := report.Providers[2]; grok.Status != StatusNeedsLogin || grok.Action != "grok login" || len(grok.Windows) != 0 {
		t.Fatalf("grok = %+v", grok)
	}
}

func TestBuildCollectReport_EmptyInputHasEmptyListsNotNull(t *testing.T) {
	raw, err := json.Marshal(BuildCollectReport(CollectReportInput{Filter: CollectFilterAll}))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	for _, key := range []string{"providers", "apiUsage", "lowCachePanes"} {
		if _, ok := doc[key].([]any); !ok {
			t.Errorf("%s = %v, want []", key, doc[key])
		}
	}
	if doc["emptyMessage"] != "(no usage data yet)" || doc["limitPercent"] != "remaining" {
		t.Fatalf("doc = %v", doc)
	}
}

func TestParseOpenPanesJSON(t *testing.T) {
	snaps, err := ParseOpenPanesJSON([]byte(`[
		{"paneId":"p1","agent":"claude","label":"api","cwd":"/repo"},
		{"paneId":"p2","agent":"codex"},
		{"paneId":"p3","agent":""}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 {
		t.Fatalf("snaps = %+v, want the two agent panes", snaps)
	}
	if snaps[0].Label != "api" || snaps[0].Cwd == nil || *snaps[0].Cwd != "/repo" {
		t.Fatalf("snaps[0] = %+v", snaps[0])
	}
	if snaps[1].Label != "codex" {
		t.Fatalf("a missing label must fall back to the agent id, got %q", snaps[1].Label)
	}
	if _, err := ParseOpenPanesJSON([]byte(`{"not":"a list"}`)); err == nil {
		t.Fatal("want an error for a non-array")
	}
	// A confirmed empty list is "no agent panes open", not a failed query.
	empty, err := ParseOpenPanesJSON([]byte(`[]`))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %v, %v", empty, err)
	}
}
