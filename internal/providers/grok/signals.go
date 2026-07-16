/**
 * Pure function that derives a ContextUsage from a Grok signals.json.
 */
package grok

import (
	"encoding/json"
	"math"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// Signals is the parsed shape of signals.json.
type Signals struct {
	ContextTokensUsed   *float64 `json:"contextTokensUsed"`
	ContextWindowTokens *float64 `json:"contextWindowTokens"`
	ContextWindowUsage  *float64 `json:"contextWindowUsage"`
}

// UsageFromSignals builds usage from a parsed signals.json object.
// Returns nil when contextTokensUsed is missing or non-positive.
func UsageFromSignals(signals Signals) *core.ContextUsage {
	if signals.ContextTokensUsed == nil || !isFinite(*signals.ContextTokensUsed) || *signals.ContextTokensUsed <= 0 {
		return nil
	}
	used := int(*signals.ContextTokensUsed)
	if signals.ContextWindowTokens != nil && isFinite(*signals.ContextWindowTokens) && *signals.ContextWindowTokens > 0 {
		w := int(*signals.ContextWindowTokens)
		return &core.ContextUsage{ContextTokens: used, WindowTokens: &w}
	}
	return &core.ContextUsage{ContextTokens: used}
}

// ParseSignalsJSON parses signals.json raw content.
func ParseSignalsJSON(raw string) (*Signals, bool) {
	var s Signals
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, false
	}
	return &s, true
}

func isFinite(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0)
}
