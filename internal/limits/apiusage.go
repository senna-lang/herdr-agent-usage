/**
 * Pay-as-you-go backend usage for the limits panel.
 *
 * Subscription providers get remaining-% windows; a pay-as-you-go backend has
 * no quota to report against, so it gets rolling spend instead: tokens (and
 * USD, when the harness records it) over 24h / 7d / 30d, a per-model
 * breakdown, and the same 24h pane share the subscription blocks show.
 *
 * Every harness can contribute observations when it has an open API-billed
 * pane; observations sharing a backend id are merged into one provider block.
 * Only OpenCode records per-message cost; Claude / Codex /
 * Grok blocks are token-only. Backend labels come from session providerIDs
 * (OpenCode, Codex), deployment env (Claude), or modelId+config.toml (Grok).
 *
 * Pure aggregation only; DB reads live in apiusage_io.go.
 */
package limits

import (
	"encoding/json"
	"sort"
	"strings"
)

// APIUsageWindow is one rolling window's spend for a backend.
type APIUsageWindow struct {
	WindowMinutes int
	Tokens        float64
	CostUSD       float64
}

// APIModelUsage is one model's contribution within a backend.
type APIModelUsage struct {
	ModelID string
	Tokens  float64
	CostUSD float64
}

// APIProviderUsage is one pay-as-you-go backend's panel block.
type APIProviderUsage struct {
	// BackendID is OpenCode's providerID ("deepseek").
	BackendID string
	// Label is the catalog display name ("DeepSeek"), else a humanized id.
	Label string
	// Windows are ordered shortest-first: 24h, 7d, 30d.
	Windows []APIUsageWindow
	// Models are the 24h per-model rows, richest-first.
	Models []APIModelUsage
	// PaneActivity is the 24h per-pane share, token-based to match the
	// subscription blocks' share rows.
	PaneActivity *ProviderPaneActivity
	// HasCost is true when any window carries nonzero spend. Local backends
	// (Ollama) report a real 0, so the cost column is dropped rather than
	// printing "$0.00" everywhere.
	HasCost bool
}

// APIUsageWindowMinutes are the panel's rolling windows, shortest-first.
var APIUsageWindowMinutes = []int{24 * 60, 7 * 24 * 60, 30 * 24 * 60}

// APIShareWindowMinutes is the window the pane-share row aggregates over.
const APIShareWindowMinutes = 24 * 60

// apiUsageRow is one decoded assistant message relevant to spend.
type apiUsageRow struct {
	CreatedMs int64
	ModelID   string
	Tokens    float64
	CostUSD   float64
}

// DecodeAPIUsageRows extracts assistant token/cost rows for one backend.
// Rows for other backends, non-assistant roles, and malformed JSON are
// dropped. Timestamps prefer the message body's time.created.
func DecodeAPIUsageRows(rows []OpenCodeTokenRow, backendID string) []apiUsageRow {
	out := make([]apiUsageRow, 0, len(rows))
	for _, row := range rows {
		var parsed struct {
			Role       string   `json:"role"`
			ProviderID string   `json:"providerID"`
			ModelID    string   `json:"modelID"`
			Cost       *float64 `json:"cost"`
			Tokens     *struct {
				Input     *float64 `json:"input"`
				Output    *float64 `json:"output"`
				Reasoning *float64 `json:"reasoning"`
				Cache     *struct {
					Read  *float64 `json:"read"`
					Write *float64 `json:"write"`
				} `json:"cache"`
			} `json:"tokens"`
			Time *struct {
				Created *float64 `json:"created"`
			} `json:"time"`
		}
		if err := json.Unmarshal([]byte(row.Data), &parsed); err != nil {
			continue
		}
		if parsed.Role != "assistant" {
			continue
		}
		if backendID != "" && parsed.ProviderID != backendID {
			continue
		}
		created := row.TimeCreated
		if parsed.Time != nil && parsed.Time.Created != nil && isFinite(*parsed.Time.Created) {
			created = int64(*parsed.Time.Created)
		}
		entry := apiUsageRow{
			CreatedMs: created,
			ModelID:   parsed.ModelID,
			CostUSD:   nonNeg(parsed.Cost),
		}
		if t := parsed.Tokens; t != nil {
			entry.Tokens = nonNeg(t.Input) + nonNeg(t.Output) + nonNeg(t.Reasoning)
			if t.Cache != nil {
				entry.Tokens += nonNeg(t.Cache.Read) + nonNeg(t.Cache.Write)
			}
		}
		out = append(out, entry)
	}
	return out
}

// SumAPIWindows totals each window from a single decoded row set, so one
// 30d fetch serves every window rather than re-scanning per window.
func SumAPIWindows(rows []apiUsageRow, nowMs int64, windowMinutes []int) []APIUsageWindow {
	out := make([]APIUsageWindow, len(windowMinutes))
	for i, mins := range windowMinutes {
		start := WindowStartMs(nowMs, mins)
		w := APIUsageWindow{WindowMinutes: mins}
		for _, r := range rows {
			if r.CreatedMs < start || r.CreatedMs > nowMs {
				continue
			}
			w.Tokens += r.Tokens
			w.CostUSD += r.CostUSD
		}
		out[i] = w
	}
	return out
}

// SumAPIModels groups rows inside the window by model, richest-first
// (cost desc, then tokens desc, then id asc for a stable order).
func SumAPIModels(rows []apiUsageRow, nowMs int64, windowMinutes int) []APIModelUsage {
	start := WindowStartMs(nowMs, windowMinutes)
	byModel := make(map[string]*APIModelUsage)
	for _, r := range rows {
		if r.CreatedMs < start || r.CreatedMs > nowMs {
			continue
		}
		if r.ModelID == "" {
			continue
		}
		entry, ok := byModel[r.ModelID]
		if !ok {
			entry = &APIModelUsage{ModelID: r.ModelID}
			byModel[r.ModelID] = entry
		}
		entry.Tokens += r.Tokens
		entry.CostUSD += r.CostUSD
	}
	out := make([]APIModelUsage, 0, len(byModel))
	for _, e := range byModel {
		if e.Tokens <= 0 && e.CostUSD <= 0 {
			continue
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CostUSD != out[j].CostUSD {
			return out[i].CostUSD > out[j].CostUSD
		}
		if out[i].Tokens != out[j].Tokens {
			return out[i].Tokens > out[j].Tokens
		}
		return out[i].ModelID < out[j].ModelID
	})
	return out
}

// AnyAPICost reports whether any window carries spend, so callers can drop
// the cost column for backends the harness prices at 0 (local models).
func AnyAPICost(windows []APIUsageWindow) bool {
	for _, w := range windows {
		if w.CostUSD > 0 {
			return true
		}
	}
	return false
}

// MergeAPIProviderUsage collapses harness-specific observations into one
// block per billed backend. Harnesses are input adapters; BackendID is the
// identity displayed and aggregated by the Usage pane.
func MergeAPIProviderUsage(blocks []APIProviderUsage) []APIProviderUsage {
	byBackend := make(map[string]*APIProviderUsage)
	order := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.BackendID == "" {
			continue
		}
		dst, ok := byBackend[block.BackendID]
		if !ok {
			dst = &APIProviderUsage{BackendID: block.BackendID, Label: block.Label}
			byBackend[block.BackendID] = dst
			order = append(order, block.BackendID)
		}
		if dst.Label == "" {
			dst.Label = block.Label
		}
		mergeAPIWindows(dst, block.Windows)
		mergeAPIModels(dst, block.Models)
		mergeAPIActivity(dst, block.PaneActivity)
	}
	out := make([]APIProviderUsage, 0, len(order))
	for _, id := range order {
		block := byBackend[id]
		sort.Slice(block.Windows, func(i, j int) bool { return block.Windows[i].WindowMinutes < block.Windows[j].WindowMinutes })
		sort.Slice(block.Models, func(i, j int) bool {
			if block.Models[i].CostUSD != block.Models[j].CostUSD {
				return block.Models[i].CostUSD > block.Models[j].CostUSD
			}
			if block.Models[i].Tokens != block.Models[j].Tokens {
				return block.Models[i].Tokens > block.Models[j].Tokens
			}
			return block.Models[i].ModelID < block.Models[j].ModelID
		})
		block.HasCost = AnyAPICost(block.Windows)
		out = append(out, *block)
	}
	return out
}

func mergeAPIWindows(dst *APIProviderUsage, src []APIUsageWindow) {
	index := make(map[int]int, len(dst.Windows))
	for i, w := range dst.Windows {
		index[w.WindowMinutes] = i
	}
	for _, w := range src {
		if i, ok := index[w.WindowMinutes]; ok {
			dst.Windows[i].Tokens += w.Tokens
			dst.Windows[i].CostUSD += w.CostUSD
		} else {
			index[w.WindowMinutes] = len(dst.Windows)
			dst.Windows = append(dst.Windows, w)
		}
	}
}

func mergeAPIModels(dst *APIProviderUsage, src []APIModelUsage) {
	index := make(map[string]int, len(dst.Models))
	for i, m := range dst.Models {
		index[m.ModelID] = i
	}
	for _, m := range src {
		if i, ok := index[m.ModelID]; ok {
			dst.Models[i].Tokens += m.Tokens
			dst.Models[i].CostUSD += m.CostUSD
		} else {
			index[m.ModelID] = len(dst.Models)
			dst.Models = append(dst.Models, m)
		}
	}
}

func mergeAPIActivity(dst *APIProviderUsage, src *ProviderPaneActivity) {
	if src == nil {
		return
	}
	if dst.PaneActivity == nil {
		dst.PaneActivity = &ProviderPaneActivity{WindowMinutes: src.WindowMinutes}
	}
	activity := dst.PaneActivity
	activity.TotalTokens += src.TotalTokens
	rows := make(map[string]PaneTokenRow)
	for _, existing := range activity.Panes {
		rows[existing.PaneID] = PaneTokenRow{PaneID: existing.PaneID, Label: existing.Label, Tokens: existing.Tokens}
	}
	for _, pane := range src.Panes {
		row := rows[pane.PaneID]
		row.PaneID = pane.PaneID
		if row.Label == "" {
			row.Label = pane.Label
		}
		row.Tokens += pane.Tokens
		rows[pane.PaneID] = row
	}
	actual := make([]PaneTokenRow, 0, len(rows))
	for id, row := range rows {
		if id != OtherPaneID {
			actual = append(actual, row)
		}
	}
	_, activity.Panes = ComputeSharesWithOther(DisambiguateLabels(actual), float64(activity.TotalTokens))
}

// HumanizeBackendID is the label fallback when a backend is absent from
// OpenCode's catalog: "my-deepseek" -> "My Deepseek".
func HumanizeBackendID(id string) string {
	if id == "" {
		return ""
	}
	parts := strings.FieldsFunc(id, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' '
	})
	for i, p := range parts {
		runes := []rune(p)
		parts[i] = strings.ToUpper(string(runes[0])) + string(runes[1:])
	}
	if len(parts) == 0 {
		return id
	}
	return strings.Join(parts, " ")
}
