/**
 * Usage shape extracted from a Codex rollout jsonl.
 */
package codex

import "github.com/senna-lang/herdr-agent-usage/internal/core"

// TokenUsage is context occupancy from a Codex rollout.
type TokenUsage struct {
	ContextTokens int
	WindowTokens  *int
	Cache         *core.CacheUsage
}
