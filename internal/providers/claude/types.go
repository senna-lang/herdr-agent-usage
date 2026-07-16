/**
 * Usage shape derived from a Claude Code transcript (Anthropic API compatible).
 */
package claude

// TranscriptUsage is the Claude assistant usage row from a transcript.
type TranscriptUsage struct {
	Model                    string
	InputTokens              int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
	OutputTokens             int
}
