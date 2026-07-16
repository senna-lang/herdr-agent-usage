/**
 * UsageProvider for Claude Code.
 * Uses the UUID from herdr's agent_session (kind === "id") as the session key.
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
	session := input.Session
	if session == nil || session.Kind != "id" || session.Value == "" {
		return nil
	}
	transcript := ResolveUsageForSession(session.Value)
	if transcript == nil {
		return nil
	}
	usage := ToContextUsage(*transcript)
	return &usage
}
