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

func sessionIDOf(input provider.UsageResolveInput) *string {
	if input.Session == nil || input.Session.Kind != "id" || input.Session.Value == "" {
		return nil
	}
	v := input.Session.Value
	return &v
}

func resolveOpenCodeUsage(input provider.UsageResolveInput) *core.ContextUsage {
	return ResolveUsageForOpenCode(sessionIDOf(input), input.Cwd)
}
