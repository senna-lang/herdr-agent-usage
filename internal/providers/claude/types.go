/**
 * Usage shape derived from a Claude Code transcript (Anthropic API compatible).
 */
package claude

import "github.com/senna-lang/herdr-agent-usage/internal/core"

// TranscriptUsage is the Claude assistant usage row from a transcript.
type TranscriptUsage struct {
	Model                    string
	InputTokens              int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
	OutputTokens             int
	// Compacted means InputTokens holds a compact_boundary's postTokens
	// estimate rather than an API-reported usage row.
	Compacted bool
	// SessionCache is the cumulative prompt-cache counters after the newest
	// compact boundary. Nil when compacted or when the session recorded none.
	// Latest-turn counters live on the row fields themselves.
	SessionCache *core.CacheUsage
}
