/**
 * Pay-as-you-go usage decoders for the non-OpenCode harnesses.
 *
 * Each returns the same []apiUsageRow the OpenCode path produces, so windows,
 * model breakdown, and pane shares are computed by the shared helpers in
 * apiusage.go rather than per harness.
 *
 * How each harness names its backend:
 *   codex  — session_meta.model_provider ("openai", "ollama-launch")
 *   claude — deployment env / settings (bedrock, vertex, foundry, gateway);
 *            transcripts themselves do not record the provider, so the
 *            fallback label is anthropic
 *   grok   — session modelId joined with ~/.grok/config.toml [model.*]
 *            base_url (openai, ollama, anthropic, …); fallback is xai
 *
 * None of these harnesses record cost, so their blocks are token-only.
 */
package limits

import (
	"encoding/json"
	"strings"
)

// CodexProviderFromLines reads session_meta.model_provider, the per-session
// backend label. Empty when the rollout has no session_meta.
func CodexProviderFromLines(lines []string) string {
	for _, line := range lines {
		raw := strings.TrimSpace(line)
		if raw == "" {
			continue
		}
		var parsed struct {
			Type    string `json:"type"`
			Payload *struct {
				ModelProvider string `json:"model_provider"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if parsed.Type == "session_meta" && parsed.Payload != nil {
			return parsed.Payload.ModelProvider
		}
	}
	return ""
}

// CodexUsageRowsFromLines converts a rollout into per-event usage rows.
//
// token_count carries total_token_usage, which is cumulative for the session,
// so each row records the delta from the previous event. Summing raw totals
// would multiply a session's usage by its event count.
func CodexUsageRowsFromLines(lines []string, modelID string) []apiUsageRow {
	var out []apiUsageRow
	prev := 0.0
	seen := false
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
		if parsed.Payload.Info == nil {
			continue
		}
		total, ok := totalFromCodexBlock(parsed.Payload.Info.TotalTokenUsage)
		if !ok {
			continue
		}
		tsMs, ok := parseIsoToMs(parsed.Timestamp)
		if !ok {
			continue
		}
		delta := total
		if seen {
			delta = total - prev
		}
		prev = total
		seen = true
		if delta <= 0 {
			continue
		}
		out = append(out, apiUsageRow{CreatedMs: tsMs, ModelID: modelID, Tokens: delta})
	}
	return out
}

// CodexModelFromLines reads the session's model id, used for the model row.
// Codex records one model per session rather than per token_count event.
func CodexModelFromLines(lines []string) string {
	for _, line := range lines {
		raw := strings.TrimSpace(line)
		if raw == "" {
			continue
		}
		var parsed struct {
			Payload *struct {
				Model string `json:"model"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if parsed.Payload != nil && parsed.Payload.Model != "" {
			return parsed.Payload.Model
		}
	}
	return ""
}

// ClaudeUsageRowsFromLines converts a session transcript into usage rows.
// Claude stamps every assistant message with its model, so the model row works
// here even though the backend label cannot vary.
func ClaudeUsageRowsFromLines(lines []string) []apiUsageRow {
	var out []apiUsageRow
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
				Model string `json:"model"`
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
		if parsed.Type != "assistant" || parsed.IsSidechain || parsed.Message == nil || parsed.Message.Usage == nil {
			continue
		}
		tsMs, ok := parseIsoToMs(parsed.Timestamp)
		if !ok {
			continue
		}
		u := parsed.Message.Usage
		tokens := nonNeg(u.InputTokens) + nonNeg(u.CacheReadInputTokens) +
			nonNeg(u.CacheCreationInputTokens) + nonNeg(u.OutputTokens)
		if tokens <= 0 {
			continue
		}
		model := parsed.Message.Model
		if model == "<synthetic>" {
			model = ""
		}
		out = append(out, apiUsageRow{CreatedMs: tsMs, ModelID: model, Tokens: tokens})
	}
	return out
}

// GrokUsageRowsFromLines converts a session's updates.jsonl into usage rows.
// Timestamps are unix seconds here, unlike the ISO strings the other harnesses
// write.
func GrokUsageRowsFromLines(lines []string) []apiUsageRow {
	var out []apiUsageRow
	for _, line := range lines {
		raw := strings.TrimSpace(line)
		if raw == "" || !strings.Contains(raw, "turn_completed") {
			continue
		}
		var parsed struct {
			Timestamp *float64 `json:"timestamp"`
			Params    *struct {
				Update *struct {
					SessionUpdate string `json:"sessionUpdate"`
					ModelID       string `json:"modelId"`
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
		if parsed.Timestamp == nil || !isFinite(*parsed.Timestamp) {
			continue
		}
		usage := update.Usage
		total := 0.0
		if usage.TotalTokens != nil && isFinite(*usage.TotalTokens) {
			total = *usage.TotalTokens
		} else {
			total = nonNeg(usage.InputTokens) + nonNeg(usage.OutputTokens) +
				nonNeg(usage.CachedReadTokens) + nonNeg(usage.ReasoningTokens)
		}
		if total <= 0 {
			continue
		}
		out = append(out, apiUsageRow{
			CreatedMs: int64(*parsed.Timestamp * 1000),
			ModelID:   update.ModelID,
			Tokens:    total,
		})
	}
	return out
}

// OMPPiUsageRowsFromLines converts an OMP / stock Pi session jsonl into
// usage rows. Each assistant turn carries its own provider/model/cost, so
// the panel can show backend spend with USD when the harness recorded it.
// Timestamps are unix milliseconds on the message.
func OMPPiUsageRowsFromLines(lines []string) []apiUsageRow {
	var out []apiUsageRow
	for _, line := range lines {
		raw := strings.TrimSpace(line)
		if raw == "" || !strings.Contains(raw, `"role"`) {
			continue
		}
		var parsed struct {
			Type    string `json:"type"`
			Message *struct {
				Role      string `json:"role"`
				Provider  string `json:"provider"`
				Model     string `json:"model"`
				Timestamp int64  `json:"timestamp"`
				Usage     *struct {
					TotalTokens *float64 `json:"totalTokens"`
					Cost        *struct {
						Total *float64 `json:"total"`
					} `json:"cost"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if parsed.Message == nil || parsed.Message.Role != "assistant" || parsed.Message.Usage == nil {
			continue
		}
		if parsed.Message.Timestamp <= 0 {
			continue
		}
		tokens := nonNeg(parsed.Message.Usage.TotalTokens)
		if tokens <= 0 {
			continue
		}
		cost := 0.0
		if parsed.Message.Usage.Cost != nil {
			cost = nonNeg(parsed.Message.Usage.Cost.Total)
		}
		out = append(out, apiUsageRow{
			CreatedMs: parsed.Message.Timestamp,
			ModelID:   parsed.Message.Model,
			Tokens:    tokens,
			CostUSD:   cost,
		})
	}
	return out
}

// OMPPiUsageRowsByBackendFromLines is like OMPPiUsageRowsFromLines, but
// groups rows by the assistant message's provider id ("deepseek", "cursor").
func OMPPiUsageRowsByBackendFromLines(lines []string) map[string][]apiUsageRow {
	out := make(map[string][]apiUsageRow)
	for _, line := range lines {
		raw := strings.TrimSpace(line)
		if raw == "" || !strings.Contains(raw, `"role"`) {
			continue
		}
		var parsed struct {
			Message *struct {
				Role      string `json:"role"`
				Provider  string `json:"provider"`
				Model     string `json:"model"`
				Timestamp int64  `json:"timestamp"`
				Usage     *struct {
					TotalTokens *float64 `json:"totalTokens"`
					Cost        *struct {
						Total *float64 `json:"total"`
					} `json:"cost"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if parsed.Message == nil || parsed.Message.Role != "assistant" || parsed.Message.Usage == nil {
			continue
		}
		if parsed.Message.Timestamp <= 0 {
			continue
		}
		backendID := strings.TrimSpace(parsed.Message.Provider)
		if backendID == "" {
			continue
		}
		tokens := nonNeg(parsed.Message.Usage.TotalTokens)
		if tokens <= 0 {
			continue
		}
		cost := 0.0
		if parsed.Message.Usage.Cost != nil {
			cost = nonNeg(parsed.Message.Usage.Cost.Total)
		}
		out[backendID] = append(out[backendID], apiUsageRow{
			CreatedMs: parsed.Message.Timestamp,
			ModelID:   parsed.Message.Model,
			Tokens:    tokens,
			CostUSD:   cost,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
