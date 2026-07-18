/**
 * UsageProvider for Grok Build.
 *
 * When no session is provided, falls back to pane cwd combined with
 * active_sessions.json / the most recent session.
 */
package grok

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

// Provider is the Grok UsageProvider.
var Provider = provider.FuncProvider{
	ID:   "grok",
	Func: resolveGrokUsage,
}

func resolveGrokUsage(input provider.UsageResolveInput) *core.ContextUsage {
	return ResolveUsageForGrok(provider.SessionID(input), input.Cwd)
}
