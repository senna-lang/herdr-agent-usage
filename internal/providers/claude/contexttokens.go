/**
 * Sums the context-occupying tokens from a Claude TranscriptUsage.
 * Includes input + cache_read + cache_creation; output is excluded.
 */
package claude

// ContextTokensOf returns input + cache_read + cache_creation (excluding output).
func ContextTokensOf(usage TranscriptUsage) int {
	return usage.InputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens
}
