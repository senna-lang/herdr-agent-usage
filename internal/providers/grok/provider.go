/**
 * UsageProvider for Grok Build.
 *
 * When no session is provided, falls back to pane cwd combined with
 * unique live entries in active_sessions.json, then the most recent
 * historical session if nothing is live.
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
