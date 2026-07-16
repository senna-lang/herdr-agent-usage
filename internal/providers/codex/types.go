/**
 * Usage shape extracted from a Codex rollout jsonl.
 */
package codex

// TokenUsage is context occupancy from a Codex rollout.
type TokenUsage struct {
	ContextTokens int
	WindowTokens  *int
}
