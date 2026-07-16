/**
 * Tests for ContextTokensOf.
 */
package claude

import "testing"

func TestContextTokensOf_ExcludesOutput(t *testing.T) {
	got := ContextTokensOf(TranscriptUsage{
		Model:                    "claude-sonnet-5",
		InputTokens:              100,
		CacheReadInputTokens:     200,
		CacheCreationInputTokens: 50,
		OutputTokens:             999,
	})
	if got != 350 {
		t.Fatalf("got %d want 350", got)
	}
}
