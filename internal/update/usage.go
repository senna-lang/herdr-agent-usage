/**
 * Resolves one safely attributed pane's session usage through provider adapters.
 *
 * Provider/profile differences remain contained here so every caller uses the
 * same ContextUsage result for context and cache presentation.
 */
package update

import (
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
)

func resolvePaneUsage(
	paneID string,
	pane herdrcli.PaneInfo,
	usageProvider provider.UsageProvider,
	providerID string,
	claudeProfiles []claude.ClaudeProfile,
	codexProfiles []codex.CodexProfile,
	grokProfiles []grok.GrokProfile,
	openCodeProfiles []opencode.OpenCodeProfile,
) *core.ContextUsage {
	if pane.Agent == nil || usageProvider == nil {
		return nil
	}

	cwd := paneCwdForUpdate(pane)
	var sessionID *string
	if pane.AgentSession != nil {
		sessionID = &pane.AgentSession.Value
	}

	switch *pane.Agent {
	case "claude":
		profile, ok := findClaudeProfile(claudeProfiles, providerID)
		if !ok || sessionID == nil {
			return nil
		}
		transcript := claude.ResolveUsageForSessionIn(profile.ProjectsRoot, *sessionID)
		if transcript == nil {
			return nil
		}
		usage := claude.ToContextUsage(*transcript)
		attachClaudePromptCacheTTL(&usage, profile.LimitsCache)
		return &usage
	case "codex":
		profile, ok := findCodexProfile(codexProfiles, providerID)
		if !ok {
			return nil
		}
		extracted := codex.ResolveUsageForCodexIn(profile.Home, sessionID, cwd)
		if extracted == nil {
			return nil
		}
		usage := core.ContextUsage{ContextTokens: extracted.ContextTokens, Cache: extracted.Cache}
		if extracted.WindowTokens != nil {
			usage.WindowTokens = extracted.WindowTokens
		}
		return &usage
	case "grok":
		profile, ok := findGrokProfile(grokProfiles, providerID)
		if !ok {
			return nil
		}
		if profile.Implicit {
			return usageProvider.ResolveUsage(provider.UsageResolveInput{Session: pane.AgentSession, Cwd: cwd, PaneID: &paneID})
		}
		return grok.ResolveUsageForGrokIn(profile.Home, sessionID, cwd)
	case "opencode":
		profile, ok := findOpenCodeProfile(openCodeProfiles, providerID)
		if !ok {
			return nil
		}
		if profile.Implicit {
			return usageProvider.ResolveUsage(provider.UsageResolveInput{Session: pane.AgentSession, Cwd: cwd, PaneID: &paneID})
		}
		return opencode.ResolveUsageForOpenCodeIn(profile.DataDir, sessionID, cwd)
	default:
		return usageProvider.ResolveUsage(provider.UsageResolveInput{
			Session: pane.AgentSession,
			Cwd:     cwd,
			PaneID:  &paneID,
		})
	}
}
