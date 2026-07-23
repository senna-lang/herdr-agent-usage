/**
 * Pure extractors for OMP session jsonl lines.
 */
package omp

import (
	"encoding/json"
	"strings"
)

type assistantLine struct {
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
		ContextSnapshot *struct {
			PromptTokens *float64 `json:"promptTokens"`
		} `json:"contextSnapshot"`
	} `json:"message"`
}

func parseAssistantLine(raw string) *assistantLine {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.Contains(raw, `"role"`) {
		return nil
	}
	var parsed assistantLine
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	if parsed.Message == nil || parsed.Message.Role != "assistant" {
		return nil
	}
	return &parsed
}

func intOrZero(n *float64) int {
	if n == nil {
		return 0
	}
	return int(*n)
}

func floatOrZero(n *float64) float64 {
	if n == nil {
		return 0
	}
	return *n
}

// ExtractLatestUsageFromLines walks jsonl lines from the end and returns the
// latest assistant row with context occupancy (or totalTokens as fallback).
func ExtractLatestUsageFromLines(lines []string) *SessionUsage {
	for i := len(lines) - 1; i >= 0; i-- {
		parsed := parseAssistantLine(lines[i])
		if parsed == nil || parsed.Message == nil {
			continue
		}
		msg := parsed.Message
		contextTokens := 0
		if msg.ContextSnapshot != nil {
			contextTokens = intOrZero(msg.ContextSnapshot.PromptTokens)
		}
		totalTokens := 0
		cost := 0.0
		if msg.Usage != nil {
			totalTokens = intOrZero(msg.Usage.TotalTokens)
			if msg.Usage.Cost != nil {
				cost = floatOrZero(msg.Usage.Cost.Total)
			}
		}
		if contextTokens <= 0 {
			contextTokens = totalTokens
		}
		if contextTokens <= 0 {
			continue
		}
		return &SessionUsage{
			Provider:      msg.Provider,
			Model:         msg.Model,
			ContextTokens: contextTokens,
			TotalTokens:   totalTokens,
			CostUSD:       cost,
		}
	}
	return nil
}

// ExtractLatestBackendFromLines returns the most recent assistant provider id.
func ExtractLatestBackendFromLines(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		parsed := parseAssistantLine(lines[i])
		if parsed == nil || parsed.Message == nil {
			continue
		}
		if provider := strings.TrimSpace(parsed.Message.Provider); provider != "" {
			return provider
		}
	}
	return ""
}

// SumUsageFromLines sums assistant turn totals. When startMs/endMs > 0, only
// events with timestamps inside [startMs, endMs] are counted. Timestamps of 0
// are always included when a window is unset (startMs==0 && endMs==0), and
// skipped when a window is active.
func SumUsageFromLines(lines []string, startMs, endMs int64) (tokens float64, costUSD float64) {
	windowed := startMs > 0 || endMs > 0
	for _, line := range lines {
		parsed := parseAssistantLine(line)
		if parsed == nil || parsed.Message == nil || parsed.Message.Usage == nil {
			continue
		}
		msg := parsed.Message
		if windowed {
			ts := msg.Timestamp
			if ts <= 0 {
				continue
			}
			if startMs > 0 && ts < startMs {
				continue
			}
			if endMs > 0 && ts > endMs {
				continue
			}
		}
		tokens += floatOrZero(msg.Usage.TotalTokens)
		if msg.Usage.Cost != nil {
			costUSD += floatOrZero(msg.Usage.Cost.Total)
		}
	}
	return tokens, costUSD
}
