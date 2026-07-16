/**
 * Reads a signals.json and returns a ContextUsage.
 */
package grok

import (
	"os"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// ResolveUsageForGrok resolves usage from session id and/or cwd.
func ResolveUsageForGrok(sessionID, cwd *string) *core.ContextUsage {
	path := ResolveSignalsPath(sessionID, cwd)
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	signals, ok := ParseSignalsJSON(string(raw))
	if !ok {
		return nil
	}
	return UsageFromSignals(*signals)
}
