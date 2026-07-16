/**
 * OpenCode MessageUsage + models.json window size -> ContextUsage.
 */
package opencode

import "github.com/senna-lang/herdr-agent-usage/internal/core"

// ToContextUsage attaches windowTokens for known models.
func ToContextUsage(usage MessageUsage) core.ContextUsage {
	window := ContextWindowFor(usage.ProviderID, usage.ModelID)
	out := core.ContextUsage{ContextTokens: usage.ContextTokens}
	if window != nil {
		out.WindowTokens = window
	}
	return out
}
