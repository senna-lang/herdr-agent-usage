/**
 * Pure extractors for OMP session jsonl lines.
 */
package omp

import (
	"encoding/json"
	"strings"
)

type assistantUsage struct {
	Input       *float64 `json:"input"`
	Output      *float64 `json:"output"`
	CacheRead   *float64 `json:"cacheRead"`
	CacheWrite  *float64 `json:"cacheWrite"`
	TotalTokens *float64 `json:"totalTokens"`
	Cost        *struct {
		Total *float64 `json:"total"`
	} `json:"cost"`
}

type assistantMessage struct {
	Role            string          `json:"role"`
	Provider        string          `json:"provider"`
	Model           string          `json:"model"`
	StopReason      string          `json:"stopReason"`
	Timestamp       int64           `json:"timestamp"`
	Usage           *assistantUsage `json:"usage"`
	ContextSnapshot *struct {
		PromptTokens *float64 `json:"promptTokens"`
	} `json:"contextSnapshot"`
}

type assistantLine struct {
	Type     string            `json:"type"`
	ID       string            `json:"id"`
	ParentID *string           `json:"parentId"`
	Message  *assistantMessage `json:"message"`
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

func totalTokensOf(usage *assistantUsage) int {
	if usage == nil {
		return 0
	}
	if total := intOrZero(usage.TotalTokens); total > 0 {
		return total
	}
	return int(floatOrZero(usage.Input) + floatOrZero(usage.Output) +
		floatOrZero(usage.CacheRead) + floatOrZero(usage.CacheWrite))
}

func sessionUsageFromMessage(msg *assistantMessage) *SessionUsage {
	if msg == nil || msg.Role != "assistant" || msg.StopReason == "aborted" || msg.StopReason == "error" {
		return nil
	}
	contextTokens := 0
	if msg.ContextSnapshot != nil {
		contextTokens = intOrZero(msg.ContextSnapshot.PromptTokens)
	}
	totalTokens := totalTokensOf(msg.Usage)
	cost := 0.0
	if msg.Usage != nil && msg.Usage.Cost != nil {
		cost = floatOrZero(msg.Usage.Cost.Total)
	}
	if contextTokens <= 0 {
		contextTokens = totalTokens
	}
	if contextTokens <= 0 {
		return nil
	}
	return &SessionUsage{
		Provider:      msg.Provider,
		Model:         msg.Model,
		ContextTokens: contextTokens,
		TotalTokens:   totalTokens,
		CostUSD:       cost,
	}
}

func extractLatestUsageLinear(lines []string) *SessionUsage {
	for i := len(lines) - 1; i >= 0; i-- {
		var entry assistantLine
		if json.Unmarshal([]byte(lines[i]), &entry) != nil {
			continue
		}
		if entry.Type == "compaction" {
			return nil
		}
		if usage := sessionUsageFromMessage(entry.Message); usage != nil {
			return usage
		}
	}
	return nil
}

// ExtractLatestUsageFromLines follows Pi's parentId tree from the most recently
// appended entry (the persisted active leaf) and returns the latest valid
// assistant usage on that branch. A compaction after the last assistant makes
// occupancy unknown until Pi records its next response, matching Pi's footer.
// Old OMP/Pi files without tree ids fall back to reverse line order.
func ExtractLatestUsageFromLines(lines []string) *SessionUsage {
	entries := make(map[string]assistantLine)
	leafID := ""
	for _, line := range lines {
		var entry assistantLine
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Type == "session" || entry.ID == "" {
			continue
		}
		entries[entry.ID] = entry
		leafID = entry.ID
	}
	if leafID == "" {
		return extractLatestUsageLinear(lines)
	}

	visited := map[string]bool{}
	for leafID != "" && !visited[leafID] {
		visited[leafID] = true
		entry, ok := entries[leafID]
		if !ok {
			// A very large trailing JSONL row can push ancestors outside the
			// bounded tail read. Preserve the old best-effort behavior then.
			return extractLatestUsageLinear(lines)
		}
		if entry.Type == "compaction" {
			return nil
		}
		if usage := sessionUsageFromMessage(entry.Message); usage != nil {
			return usage
		}
		if entry.ParentID == nil {
			return nil
		}
		leafID = *entry.ParentID
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

// SumUsageFromLines sums assistant turn totals across all backends. When
// startMs/endMs > 0, only events with timestamps inside [startMs, endMs] are
// counted. Timestamps of 0 are always included when a window is unset
// (startMs==0 && endMs==0), and skipped when a window is active.
func SumUsageFromLines(lines []string, startMs, endMs int64) (tokens float64, costUSD float64) {
	return SumUsageForProviderFromLines(lines, "", startMs, endMs)
}

// SumUsageForProviderFromLines sums assistant turn totals for one backend.
// provider is empty to include all backends.
func SumUsageForProviderFromLines(lines []string, provider string, startMs, endMs int64) (tokens float64, costUSD float64) {
	provider = strings.TrimSpace(provider)
	windowed := startMs > 0 || endMs > 0
	for _, line := range lines {
		parsed := parseAssistantLine(line)
		if parsed == nil || parsed.Message == nil || parsed.Message.Usage == nil {
			continue
		}
		msg := parsed.Message
		if provider != "" && strings.TrimSpace(msg.Provider) != provider {
			continue
		}
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
		tokens += float64(totalTokensOf(msg.Usage))
		if msg.Usage.Cost != nil {
			costUSD += floatOrZero(msg.Usage.Cost.Total)
		}
	}
	return tokens, costUSD
}
