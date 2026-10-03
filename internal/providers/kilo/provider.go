/**
 * UsageProvider for Kilo Code.
 *
 * Herdr's Kilo integration reports agent_session.kind=id with a `ses_…` id,
 * the same shape Codex and OpenCode report, so resolution is keyed on that id
 * and falls back to the pane cwd only when the reported id no longer exists.
 */
package kilo

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

// Provider is the Kilo Code UsageProvider.
var Provider = provider.FuncProvider{
	ID:   "kilo",
	Func: resolveKiloUsage,
}

func resolveKiloUsage(input provider.UsageResolveInput) *core.ContextUsage {
	return ResolveUsageForKilo(provider.SessionID(input), input.Cwd)
}
