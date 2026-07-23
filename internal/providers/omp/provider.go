/**
 * UsageProvider for OMP and stock Pi coding agent.
 *
 * Herdr integrations report agent_session.kind="path" pointing at a session
 * jsonl under ~/.omp or ~/.pi. When the path is missing (extension not yet
 * reloaded), we fall back to the newest jsonl for the pane cwd.
 */
package omp

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

// Provider is the OMP UsageProvider (Herdr agent id "omp").
var Provider = provider.FuncProvider{
	ID:   "omp",
	Func: resolveOMPUsage,
}

// PiProvider is the stock Pi coding agent UsageProvider (Herdr agent id "pi").
var PiProvider = provider.FuncProvider{
	ID:   "pi",
	Func: resolvePiUsage,
}

func resolveOMPUsage(input provider.UsageResolveInput) *core.ContextUsage {
	path := SessionPathFromInput(input)
	if path == "" && input.Cwd != nil {
		path = FindLatestOMPSessionForCwd(*input.Cwd)
	}
	if path == "" {
		return nil
	}
	return ResolveUsageForPath(path)
}

func resolvePiUsage(input provider.UsageResolveInput) *core.ContextUsage {
	path := SessionPathFromInput(input)
	if path == "" && input.Cwd != nil {
		path = FindLatestPiSessionForCwd(*input.Cwd)
	}
	if path == "" {
		return nil
	}
	return ResolveUsageForPath(path)
}
