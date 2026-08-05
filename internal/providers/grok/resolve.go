/**
 * Resolves Grok context usage for a pane.
 *
 * Sources (same session only — never a sibling under the same cwd):
 *  1. signals.json contextTokensUsed — the UI window meter ("Nk / 500K")
 *  2. updates.jsonl `_meta.totalTokens` — only when signals are missing
 *
 * The last `_meta.totalTokens` in updates.jsonl is NOT the UI window meter:
 * it often sits far below the real context (or near a pre-compaction total).
 * When signals exist, use them directly — never let last totalTokens override.
 */
package grok

import (
	"os"
	"path/filepath"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// ResolveUsageForGrok resolves usage from session id and/or cwd.
func ResolveUsageForGrok(sessionID, cwd *string) *core.ContextUsage {
	dir := ResolveSessionDir(sessionID, cwd)
	if dir == "" {
		return nil
	}
	return UsageFromSessionDir(dir)
}

// UsageFromSessionDir builds usage from one session directory's signals and/or updates.
func UsageFromSessionDir(dir string) *core.ContextUsage {
	if dir == "" {
		return nil
	}
	sigPath := filepath.Join(dir, "signals.json")
	if st, err := os.Stat(sigPath); err == nil && st.Mode().IsRegular() {
		if raw, err := os.ReadFile(sigPath); err == nil {
			if s, ok := ParseSignalsJSON(string(raw)); ok {
				if u := UsageFromSignals(*s); u != nil {
					return u
				}
			}
		}
	}

	// No usable signals: fall back to live stream meter only.
	meta, _ := LatestContextTokensFromUpdates(filepath.Join(dir, "updates.jsonl"))
	if meta > 0 {
		return &core.ContextUsage{ContextTokens: meta}
	}
	return nil
}
