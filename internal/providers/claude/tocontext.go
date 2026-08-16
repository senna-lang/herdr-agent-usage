/**
 * Maps Claude-specific TranscriptUsage to the core ContextUsage.
 */
package claude

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// ToContextUsage maps TranscriptUsage to core.ContextUsage.
func ToContextUsage(usage TranscriptUsage) core.ContextUsage {
	contextTokens := ContextTokensOf(usage)
	window := ContextWindowFor(usage.Model)
	if window == nil {
		return core.ContextUsage{ContextTokens: contextTokens, Compacted: usage.Compacted}
	}
	w := *window
	return core.ContextUsage{ContextTokens: contextTokens, WindowTokens: &w, Compacted: usage.Compacted}
}
