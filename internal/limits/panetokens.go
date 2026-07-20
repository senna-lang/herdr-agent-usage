/**
 * Per-provider: pure helpers that aggregate windowed session tokens.
 * I/O is left to the caller; only row arrays / JSON fragments are handled.
 */
package limits

import (
	"encoding/json"
	"math"
	"strings"
	"time"
)

// SumClaudeTokensInWindow sums assistant usage within the window.
// input + cache_read + cache_creation + output.
func SumClaudeTokensInWindow(lines []string, windowStartMs, windowEndMs int64) float64 {
	var sum float64
	for _, line := range lines {
		raw := strings.TrimSpace(line)
		if raw == "" {
			continue
		}
		var parsed struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			Timestamp   string `json:"timestamp"`
			Message     *struct {
				Usage *struct {
					InputTokens              *float64 `json:"input_tokens"`
					CacheReadInputTokens     *float64 `json:"cache_read_input_tokens"`
					CacheCreationInputTokens *float64 `json:"cache_creation_input_tokens"`
					OutputTokens             *float64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if parsed.Type != "assistant" || parsed.IsSidechain {
			continue
		}
		tsMs, ok := parseIsoToMs(parsed.Timestamp)
		if !ok || tsMs < windowStartMs || tsMs > windowEndMs {
			continue
		}
		if parsed.Message == nil || parsed.Message.Usage == nil {
			continue
		}
		u := parsed.Message.Usage
		sum += nonNeg(u.InputTokens)
		sum += nonNeg(u.CacheReadInputTokens)
		sum += nonNeg(u.CacheCreationInputTokens)
		sum += nonNeg(u.OutputTokens)
	}
	return sum
}

// SumCodexTokensInWindow returns the in-window delta of token_count's total_token_usage.
// Treats the latest cumulative value before the window as the baseline.
func SumCodexTokensInWindow(lines []string, windowStartMs, windowEndMs int64) float64 {
	var baseline *float64
	var lastInWindow *float64

	for _, line := range lines {
		raw := strings.TrimSpace(line)
		if raw == "" {
			continue
		}
		var parsed struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   *struct {
				Type string `json:"type"`
				Info *struct {
					TotalTokenUsage *struct {
						TotalTokens           *float64 `json:"total_tokens"`
						InputTokens           *float64 `json:"input_tokens"`
						OutputTokens          *float64 `json:"output_tokens"`
						CachedInputTokens     *float64 `json:"cached_input_tokens"`
						ReasoningOutputTokens *float64 `json:"reasoning_output_tokens"`
					} `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if parsed.Type != "event_msg" || parsed.Payload == nil || parsed.Payload.Type != "token_count" {
			continue
		}
		tsMs, ok := parseIsoToMs(parsed.Timestamp)
		if !ok {
			continue
		}
		var block *struct {
			TotalTokens           *float64 `json:"total_tokens"`
			InputTokens           *float64 `json:"input_tokens"`
			OutputTokens          *float64 `json:"output_tokens"`
			CachedInputTokens     *float64 `json:"cached_input_tokens"`
			ReasoningOutputTokens *float64 `json:"reasoning_output_tokens"`
		}
		if parsed.Payload.Info != nil {
			block = parsed.Payload.Info.TotalTokenUsage
		}
		total, ok := totalFromCodexBlock(block)
		if !ok {
			continue
		}
		if tsMs < windowStartMs {
			baseline = &total
			continue
		}
		if tsMs > windowEndMs {
			continue
		}
		lastInWindow = &total
	}

	if lastInWindow == nil {
		return 0
	}
	base := 0.0
	if baseline != nil {
		base = *baseline
	}
	delta := *lastInWindow - base
	if delta > 0 {
		return delta
	}
	return 0
}

func totalFromCodexBlock(block *struct {
	TotalTokens           *float64 `json:"total_tokens"`
	InputTokens           *float64 `json:"input_tokens"`
	OutputTokens          *float64 `json:"output_tokens"`
	CachedInputTokens     *float64 `json:"cached_input_tokens"`
	ReasoningOutputTokens *float64 `json:"reasoning_output_tokens"`
}) (float64, bool) {
	if block == nil {
		return 0, false
	}
	if block.TotalTokens != nil && isFinite(*block.TotalTokens) {
		return *block.TotalTokens, true
	}
	sum := nonNeg(block.InputTokens) + nonNeg(block.OutputTokens) +
		nonNeg(block.CachedInputTokens) + nonNeg(block.ReasoningOutputTokens)
	if sum > 0 {
		return sum, true
	}
	return 0, false
}

// OpenCodeTokenRow is one OpenCode message row for window aggregation.
type OpenCodeTokenRow struct {
	Data        string
	TimeCreated int64
}

// SumOpenCodeTokensInWindow sums assistant tokens within the window.
// input + cache.read + cache.write + output + reasoning.
func SumOpenCodeTokensInWindow(rows []OpenCodeTokenRow, windowStartMs, windowEndMs int64) float64 {
	return SumOpenCodeProviderTokensInWindow(rows, "", windowStartMs, windowEndMs)
}

// SumOpenCodeProviderTokensInWindow sums assistant tokens within the window
// for one backend providerID ("" = all). The limits pane activity passes
// "opencode-go" so direct-API traffic (deepseek, ollama, …) never counts
// against the Go plan's budget share.
func SumOpenCodeProviderTokensInWindow(rows []OpenCodeTokenRow, providerID string, windowStartMs, windowEndMs int64) float64 {
	tokens, _ := SumOpenCodeActivityInWindow(rows, providerID, windowStartMs, windowEndMs)
	return tokens
}

// SumOpenCodeActivityInWindow sums assistant tokens and USD cost within the
// window for one backend providerID ("" = all), in a single pass. OpenCode
// stamps every assistant message with a cost (from its own model-pricing
// catalog; 0 for uncataloged/local models), so this works for any backend
// OpenCode has pricing data for — not just opencode-go.
func SumOpenCodeActivityInWindow(rows []OpenCodeTokenRow, providerID string, windowStartMs, windowEndMs int64) (tokens float64, costUSD float64) {
	for _, row := range rows {
		if row.TimeCreated < windowStartMs || row.TimeCreated > windowEndMs {
			continue
		}
		var parsed struct {
			Role       string   `json:"role"`
			ProviderID string   `json:"providerID"`
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
		if providerID != "" && parsed.ProviderID != providerID {
			continue
		}
		created := row.TimeCreated
		if parsed.Time != nil && parsed.Time.Created != nil && isFinite(*parsed.Time.Created) {
			created = int64(*parsed.Time.Created)
		}
		if created < windowStartMs || created > windowEndMs {
			continue
		}
		costUSD += nonNeg(parsed.Cost)
		if parsed.Tokens == nil {
			continue
		}
		t := parsed.Tokens
		tokens += nonNeg(t.Input)
		tokens += nonNeg(t.Output)
		tokens += nonNeg(t.Reasoning)
		if t.Cache != nil {
			tokens += nonNeg(t.Cache.Read)
			tokens += nonNeg(t.Cache.Write)
		}
	}
	return tokens, costUSD
}

// SumGrokTokensInWindow sums turn_completed usage.totalTokens within the window.
// Timestamps are unix seconds.
func SumGrokTokensInWindow(lines []string, windowStartMs, windowEndMs int64) float64 {
	var sum float64
	for _, line := range lines {
		raw := strings.TrimSpace(line)
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "turn_completed") || !strings.Contains(raw, "usage") {
			continue
		}
		var parsed struct {
			Timestamp *float64 `json:"timestamp"`
			Params    *struct {
				Update *struct {
					SessionUpdate string `json:"sessionUpdate"`
					Usage         *struct {
						TotalTokens      *float64 `json:"totalTokens"`
						InputTokens      *float64 `json:"inputTokens"`
						OutputTokens     *float64 `json:"outputTokens"`
						CachedReadTokens *float64 `json:"cachedReadTokens"`
						ReasoningTokens  *float64 `json:"reasoningTokens"`
					} `json:"usage"`
				} `json:"update"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if parsed.Params == nil || parsed.Params.Update == nil {
			continue
		}
		update := parsed.Params.Update
		if update.SessionUpdate != "turn_completed" || update.Usage == nil {
			continue
		}
		usage := update.Usage
		if parsed.Timestamp == nil || !isFinite(*parsed.Timestamp) {
			continue
		}
		tsMs := int64(*parsed.Timestamp * 1000)
		if tsMs < windowStartMs || tsMs > windowEndMs {
			continue
		}
		var total float64
		if usage.TotalTokens != nil && isFinite(*usage.TotalTokens) {
			total = *usage.TotalTokens
		} else {
			total = nonNeg(usage.InputTokens) + nonNeg(usage.OutputTokens) +
				nonNeg(usage.CachedReadTokens) + nonNeg(usage.ReasoningTokens)
		}
		if total > 0 {
			sum += total
		}
	}
	return sum
}

func nonNeg(n *float64) float64 {
	if n == nil || !isFinite(*n) || *n <= 0 {
		return 0
	}
	return *n
}

func isFinite(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0)
}

func parseIsoToMs(iso string) (int64, bool) {
	if iso == "" {
		return 0, false
	}
	// RFC3339 / RFC3339Nano cover Date.toISOString() and common variants.
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, iso); err == nil {
			return t.UnixMilli(), true
		}
	}
	return 0, false
}
