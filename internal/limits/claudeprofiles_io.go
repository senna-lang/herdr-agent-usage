/**
 * Resolves configured harness profiles from plugin config for read-side
 * collectors, billing, sidebar context, and activity attribution.
 */
package limits

import (
	"os"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
	"github.com/senna-lang/herdr-agent-usage/internal/setup"
)

func processEnvMap() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	return env
}

// ResolvedClaudeProfiles resolves the configured Claude profiles (synthesizing
// the single implicit default when none are configured) from process env.
func ResolvedClaudeProfiles() []claude.ClaudeProfile {
	return setup.ResolveClaudeProfiles(processEnvMap())
}

// ResolvedLimitPercent is the plugin-config presentation direction for quota
// percentages. Unknown or missing values are remaining.
func ResolvedLimitPercent() core.LimitPercent {
	cfg := setup.LoadPluginConfig(setup.ResolvePluginConfigDir(processEnvMap()))
	return cfg.LimitPercent
}

// profileByIDIn looks up one profile by provider id within an already-resolved
// snapshot, so a caller that resolved the profiles once (e.g. per
// AttachPaneActivity pass) can dispatch without re-reading config/env per hit.
func profileByIDIn(profiles []claude.ClaudeProfile, id string) (claude.ClaudeProfile, bool) {
	for _, p := range profiles {
		if p.ID == id {
			return p, true
		}
	}
	return claude.ClaudeProfile{}, false
}

// claudeProfileByID looks up one resolved profile by provider id, resolving the
// snapshot fresh. Used by non-hot direct callers; hot loops instead capture one
// snapshot and dispatch via profileByIDIn.
func claudeProfileByID(id string) (claude.ClaudeProfile, bool) {
	return profileByIDIn(ResolvedClaudeProfiles(), id)
}

// applyProfileGrouping nests pl under the shared "Claude" heading when
// multiProfile is true, so 2+ configured accounts render as one group instead
// of N separate top-level blocks. AccountLabel carries p's real logged-in
// email so the nested row stays distinguishable even when the profile has no
// explicit label; it falls back to pl.Label when the email can't be read.
func applyProfileGrouping(pl ProviderLimits, p claude.ClaudeProfile, multiProfile bool) ProviderLimits {
	if !multiProfile {
		return pl
	}
	pl.GroupLabel = "Claude"
	if email, ok := AccountEmailFromJSONPath(p.JSONPath); ok {
		pl.AccountLabel = email
	} else {
		pl.AccountLabel = pl.Label
	}
	return pl
}

// ResolvedCodexProfiles resolves the configured Codex profiles (synthesizing
// the single implicit default when none are configured) from process env.
func ResolvedCodexProfiles() []codex.CodexProfile {
	return setup.ResolveCodexProfiles(processEnvMap())
}

func configuredCodexHomes() []string {
	profiles := ResolvedCodexProfiles()
	homes := make([]string, 0, len(profiles))
	seen := map[string]bool{}
	for _, p := range profiles {
		if p.Home == "" || seen[p.Home] {
			continue
		}
		seen[p.Home] = true
		homes = append(homes, p.Home)
	}
	return homes
}

func resolveCodexSessionAcrossHomes(sessionID, cwd *string) string {
	homes := configuredCodexHomes()
	if sessionID != nil && *sessionID != "" {
		for _, home := range homes {
			if path := codex.FindSessionFileIn(home, *sessionID); path != "" {
				return path
			}
			if path := codex.FindSessionFileByMetaIDIn(home, *sessionID); path != "" {
				return path
			}
		}
		return ""
	}
	// Cwd fallback is only safe for a single home: two accounts can share a
	// project directory, so guessing across homes would misattribute the pane.
	if len(homes) == 1 && cwd != nil && *cwd != "" {
		return codex.FindLatestSessionFileForCwdIn(homes[0], *cwd)
	}
	return ""
}

func codexProfileByIDIn(profiles []codex.CodexProfile, id string) (codex.CodexProfile, bool) {
	for _, p := range profiles {
		if p.ID == id {
			return p, true
		}
	}
	return codex.CodexProfile{}, false
}

// applyCodexProfileGrouping nests pl under the shared "Codex" heading when
// multiProfile is true. AccountLabel carries the configured label (or id);
// auth.json has no email we are willing to decode here.
func applyCodexProfileGrouping(pl ProviderLimits, p codex.CodexProfile, multiProfile bool) ProviderLimits {
	if !multiProfile {
		return pl
	}
	pl.GroupLabel = "Codex"
	pl.AccountLabel = p.Label
	return pl
}

// applyGrokProfileGrouping nests configured Grok accounts beneath one family
// heading while retaining the configured account label on each row.
func applyGrokProfileGrouping(pl ProviderLimits, profile grok.GrokProfile, multiProfile bool) ProviderLimits {
	if !multiProfile {
		return pl
	}
	pl.GroupLabel = "Grok"
	pl.AccountLabel = profile.Label
	return pl
}

// applyOpenCodeProfileGrouping nests configured OpenCode accounts beneath one
// family heading while retaining the configured account label on each row.
func applyOpenCodeProfileGrouping(pl ProviderLimits, profile opencode.OpenCodeProfile, multiProfile bool) ProviderLimits {
	if !multiProfile {
		return pl
	}
	pl.GroupLabel = "OpenCode"
	pl.AccountLabel = profile.Label
	return pl
}

func ResolvedGrokProfiles() []grok.GrokProfile {
	return setup.ResolveGrokProfiles(processEnvMap())
}

func ResolvedOpenCodeProfiles() []opencode.OpenCodeProfile {
	return setup.ResolveOpenCodeProfiles(processEnvMap())
}

func grokProfileByIDIn(profiles []grok.GrokProfile, id string) (grok.GrokProfile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return grok.GrokProfile{}, false
}

func openCodeProfileByIDIn(profiles []opencode.OpenCodeProfile, id string) (opencode.OpenCodeProfile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return opencode.OpenCodeProfile{}, false
}
