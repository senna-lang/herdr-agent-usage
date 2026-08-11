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
	if path != "" {
		if usage := ResolveUsageForPath(path); usage != nil {
			return usage
		}
		if input.Cwd == nil {
			return nil
		}

		// Resumptions can preserve a historical absolute path after OMP moves the
		// transcript into the current cwd's directory. Recover only the exact
		// filename; selecting that cwd's newest transcript could cross panes.
		return ResolveUsageForPath(FindOMPSessionForCwdByFilename(*input.Cwd, path))
	}

	// An ID-only session cannot be associated with an OMP jsonl file. Do not
	// substitute the cwd's newest file: concurrent panes routinely share a cwd.
	if input.Session != nil || input.Cwd == nil {
		return nil
	}
	return ResolveUsageForPath(FindLatestOMPSessionForCwd(*input.Cwd))
}

func resolvePiUsage(input provider.UsageResolveInput) *core.ContextUsage {
	path := SessionPathFromInput(input)
	if path == "" && input.Cwd != nil {
		path = FindLatestPiSessionForCwd(*input.Cwd)
	}
	if path == "" {
		return nil
	}
	return ResolvePiUsageForPath(path)
}
