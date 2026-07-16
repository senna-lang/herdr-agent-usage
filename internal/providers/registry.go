/**
 * UsageProvider registry for the supported agents.
 * To add a new agent, register it here and add a providers/<agent>/ directory.
 */
package providers

import (
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
)

// All registered providers.
var All = []provider.UsageProvider{
	claude.Provider,
	codex.Provider,
	grok.Provider,
	opencode.Provider,
}

// FindProvider returns the provider for agentId, or nil when unregistered.
func FindProvider(agentID string) provider.UsageProvider {
	for _, p := range All {
		if p.AgentID() == agentID {
			return p
		}
	}
	return nil
}
