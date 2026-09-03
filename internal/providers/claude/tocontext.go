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
	out := core.ContextUsage{ContextTokens: contextTokens, Compacted: usage.Compacted}
	if window := ContextWindowFor(usage.Model); window != nil {
		w := *window
		out.WindowTokens = &w
	}
	if !usage.Compacted {
		out.Cache = core.CacheFromTokenCounts(usage.InputTokens, usage.CacheReadInputTokens, usage.CacheCreationInputTokens)
		out.SessionCache = usage.SessionCache
	}
	return out
}
