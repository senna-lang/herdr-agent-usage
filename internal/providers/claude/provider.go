/**
 * UsageProvider for Claude Code.
 * Uses the UUID from herdr's agent_session (kind === "id") as the session key,
 * falling back to the newest transcript for the pane's cwd.
 */
package claude

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

// Provider is the Claude UsageProvider.
var Provider = provider.FuncProvider{
	ID:   "claude",
	Func: resolveClaudeUsage,
}

func resolveClaudeUsage(input provider.UsageResolveInput) *core.ContextUsage {
	var transcript *TranscriptUsage
	if sid := provider.SessionID(input); sid != nil {
		transcript = ResolveUsageForSession(*sid)
	}
	// No session ID (snatched/adopted panes) or its transcript is gone: follow
	// the most recently active session in the pane's cwd, like codex does.
	if transcript == nil && input.Cwd != nil {
		transcript = ResolveLatestUsageForCwd(*input.Cwd)
	}
	if transcript == nil {
		return nil
	}
	usage := ToContextUsage(*transcript)
	return &usage
}
