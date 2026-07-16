/**
 * Boundary types for per-agent usage providers.
 */
package provider

import "github.com/senna-lang/herdr-agent-usage/internal/core"

// AgentSession carries herdr's pane get agent_session verbatim.
// Interpreting kind/value (UUID vs path, etc.) is the provider's job.
type AgentSession struct {
	Kind  string
	Value string
}

// UsageResolveInput is the resolution input passed to a provider.
// The cwd is also passed for agents whose herdr integration does not
// report agent_session (currently Grok, etc.).
type UsageResolveInput struct {
	Session *AgentSession
	Cwd     *string
}

// UsageProvider resolves usage for a single agent.
// AgentID is matched against herdr's pane.agent (e.g. "claude", "codex", "grok").
type UsageProvider interface {
	AgentID() string
	ResolveUsage(input UsageResolveInput) *core.ContextUsage
}

// FuncProvider is a function-backed UsageProvider.
type FuncProvider struct {
	ID   string
	Func func(UsageResolveInput) *core.ContextUsage
}

func (p FuncProvider) AgentID() string { return p.ID }

func (p FuncProvider) ResolveUsage(input UsageResolveInput) *core.ContextUsage {
	return p.Func(input)
}
