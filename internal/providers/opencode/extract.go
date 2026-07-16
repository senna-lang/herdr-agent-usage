/**
 * Pure function that pulls a usage candidate from an OpenCode message.data JSON.
 */
package opencode

import (
	"encoding/json"
	"math"
)

// MessageUsage is tokens and model identifiers extracted from a message.
// The window size is resolved separately via models.json.
type MessageUsage struct {
	ContextTokens int
	ModelID       *string
	ProviderID    *string
}

// ContextTokensFromMessageTokens returns input + cache.read + cache.write.
// As with Claude, output / reasoning are not included.
func ContextTokensFromMessageTokens(input, cacheRead, cacheWrite float64) *int {
	in := finiteOrZero(input)
	cr := finiteOrZero(cacheRead)
	cw := finiteOrZero(cacheWrite)
	total := int(in + cr + cw)
	if total > 0 {
		return &total
	}
	return nil
}

func finiteOrZero(n float64) float64 {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0
	}
	return n
}

// UsageFromMessageData returns usage when role is assistant and tokens are present.
func UsageFromMessageData(raw map[string]any) *MessageUsage {
	role, _ := raw["role"].(string)
	if role != "assistant" {
		return nil
	}
	tokens, _ := raw["tokens"].(map[string]any)
	if tokens == nil {
		return nil
	}
	input := asFloat(tokens["input"])
	var cacheRead, cacheWrite float64
	if cache, ok := tokens["cache"].(map[string]any); ok {
		cacheRead = asFloat(cache["read"])
		cacheWrite = asFloat(cache["write"])
	}
	ct := ContextTokensFromMessageTokens(input, cacheRead, cacheWrite)
	if ct == nil {
		return nil
	}
	return &MessageUsage{
		ContextTokens: *ct,
		ModelID:       modelIDOf(raw),
		ProviderID:    providerIDOf(raw),
	}
}

func modelIDOf(data map[string]any) *string {
	if s, ok := data["modelID"].(string); ok && s != "" {
		return &s
	}
	if model, ok := data["model"].(map[string]any); ok {
		if s, ok := model["modelID"].(string); ok && s != "" {
			return &s
		}
	}
	return nil
}

func providerIDOf(data map[string]any) *string {
	if s, ok := data["providerID"].(string); ok && s != "" {
		return &s
	}
	if model, ok := data["model"].(map[string]any); ok {
		if s, ok := model["providerID"].(string); ok && s != "" {
			return &s
		}
	}
	return nil
}

func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

// UsageFromLatestMessageJSONs uses the first assistant tokens row from the
// start of rows (rows are newest-first).
func UsageFromLatestMessageJSONs(rows []string) *MessageUsage {
	for _, raw := range rows {
		var data map[string]any
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			continue
		}
		if usage := UsageFromMessageData(data); usage != nil {
			return usage
		}
	}
	return nil
}
