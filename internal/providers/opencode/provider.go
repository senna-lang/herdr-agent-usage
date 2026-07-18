/**
 * UsageProvider for OpenCode.
 *
 * When the herdr integration is present, uses the session id from
 * agent_session.kind=id. Otherwise, matches session.directory against the
 * pane cwd and picks the most recent session.
 */
package opencode

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

// Provider is the OpenCode UsageProvider.
var Provider = provider.FuncProvider{
	ID:   "opencode",
	Func: resolveOpenCodeUsage,
}

func resolveOpenCodeUsage(input provider.UsageResolveInput) *core.ContextUsage {
	return ResolveUsageForOpenCode(provider.SessionID(input), input.Cwd)
}
