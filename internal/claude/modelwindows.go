/**
 * Resolves a Claude model ID to its context window limit.
 * Real model strings carry date suffixes and Bedrock/Vertex prefixes, so
 * resolution uses longest-match substring lookup rather than exact match.
 *
 * Leaving a Claude Code paid-plan 1M-default model pinned at 200k makes
 * the % display roughly 5x too large (e.g. 130k/200k=65% vs 130k/1M~=13%).
 *
 * Always list the most specific ID (`claude-sonnet-4-5` rather than
 * `claude-sonnet-4`). The longest-key-first resolution keeps parent/child
 * prefixes from being matched incorrectly.
 */
package claude

import (
	"regexp"
	"sort"
	"strings"
)

var contextWindowTokens = map[string]int{
	// --- 1M (Claude Code paid default / OpenCode anthropic catalog) ---
	"claude-sonnet-5":   1_000_000,
	"claude-fable-5":    1_000_000,
	"claude-opus-4-8":   1_000_000,
	"claude-opus-4-7":   1_000_000,
	"claude-opus-4-6":   1_000_000,
	"claude-sonnet-4-6": 1_000_000,
	"claude-sonnet-4-5": 1_000_000,
	// Bare "sonnet-4" (no date). Shorter than sonnet-4-5/4-6, so longest-first defers to them.
	"claude-sonnet-4": 1_000_000,

	// --- 200k ---
	"claude-haiku-4-5": 200_000,
	"claude-3-5-haiku": 200_000,
	"claude-opus-4-5":  200_000,
	"claude-opus-4-1":  200_000,
}

// Try longer keys first so partial matches cannot pick up the wrong entry.
var contextWindowKeysLongestFirst []string

func init() {
	keys := make([]string, 0, len(contextWindowTokens))
	for k := range contextWindowTokens {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j])
	})
	contextWindowKeysLongestFirst = keys
}

var oneMSuffix = regexp.MustCompile(`(?i)\[1m\]$`)

// NormalizeClaudeModelID strips the `claude-opus-4-8[1m]`-style suffix that usage caches can attach.
func NormalizeClaudeModelID(model string) string {
	return strings.TrimSpace(oneMSuffix.ReplaceAllString(model, ""))
}

// ContextWindowFor returns nil for unknown models, so the caller falls back to
// the absolute token count alone.
func ContextWindowFor(model string) *int {
	normalized := NormalizeClaudeModelID(model)
	for _, key := range contextWindowKeysLongestFirst {
		if strings.Contains(normalized, key) {
			v := contextWindowTokens[key]
			return &v
		}
	}
	return nil
}
