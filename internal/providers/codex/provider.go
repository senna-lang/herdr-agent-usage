/**
 * UsageProvider for Codex.
 *
 * Uses herdr's agent_session (kind=id) to find rollout-*-{id}.jsonl under
 * ~/.codex/sessions. Falls back to the most recent rollout whose session_meta.cwd
 * matches the pane's cwd when the ID drifts.
 */
package codex

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

// Provider is the Codex UsageProvider.
var Provider = provider.FuncProvider{
	ID:   "codex",
	Func: resolveCodexUsage,
}

func resolveCodexUsage(input provider.UsageResolveInput) *core.ContextUsage {
	usage := ResolveUsageForCodex(provider.SessionID(input), input.Cwd)
	if usage == nil {
		return nil
	}
	out := core.ContextUsage{ContextTokens: usage.ContextTokens}
	if usage.WindowTokens != nil {
		out.WindowTokens = usage.WindowTokens
	}
	return &out
}
