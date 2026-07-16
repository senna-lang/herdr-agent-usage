/**
 * Reads token information equivalent to ContextUsage from a Codex session ID or cwd.
 */
package codex

import "github.com/senna-lang/herdr-agent-usage/internal/fsutil"

const tailScanBytes = 512 * 1024

// ResolveUsageForCodex resolves usage from sessionId and/or cwd.
func ResolveUsageForCodex(sessionID, cwd *string) *TokenUsage {
	path := ResolveSessionFile(sessionID, cwd)
	if path == "" {
		return nil
	}
	lines, err := fsutil.ReadLastNLines(path, tailScanBytes)
	if err != nil {
		return nil
	}
	return ExtractLatestUsageFromLines(lines)
}
