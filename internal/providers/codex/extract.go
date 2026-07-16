/**
 * Pure function that pulls the latest token_count from a Codex rollout's jsonl lines.
 */
package codex

import (
	"encoding/json"
	"math"
	"strings"
)

// ExtractLatestUsageFromLines walks lines from the end and returns the most
// recent event_msg / token_count row that contains real token counts.
// Prefers last_token_usage for context occupancy (not cumulative total_token_usage).
func ExtractLatestUsageFromLines(lines []string) *TokenUsage {
	for i := len(lines) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(lines[i])
		if raw == "" {
			continue
		}
		var parsed struct {
			Type    string `json:"type"`
			Payload *struct {
				Type string `json:"type"`
				Info *struct {
					LastTokenUsage *struct {
						InputTokens *float64 `json:"input_tokens"`
						TotalTokens *float64 `json:"total_tokens"`
					} `json:"last_token_usage"`
					ModelContextWindow *float64 `json:"model_context_window"`
				} `json:"info"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if parsed.Type != "event_msg" || parsed.Payload == nil || parsed.Payload.Type != "token_count" {
			continue
		}
		info := parsed.Payload.Info
		if info == nil {
			continue
		}
		contextTokens, ok := contextTokensFrom(info.LastTokenUsage)
		if !ok || contextTokens <= 0 {
			continue
		}
		var window *int
		if info.ModelContextWindow != nil && isFinite(*info.ModelContextWindow) && *info.ModelContextWindow > 0 {
			w := int(*info.ModelContextWindow)
			window = &w
		}
		return &TokenUsage{ContextTokens: contextTokens, WindowTokens: window}
	}
	return nil
}

func contextTokensFrom(block *struct {
	InputTokens *float64 `json:"input_tokens"`
	TotalTokens *float64 `json:"total_tokens"`
}) (int, bool) {
	if block == nil {
		return 0, false
	}
	if block.TotalTokens != nil && isFinite(*block.TotalTokens) {
		return int(*block.TotalTokens), true
	}
	if block.InputTokens != nil && isFinite(*block.InputTokens) {
		return int(*block.InputTokens), true
	}
	return 0, false
}

func isFinite(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0)
}
