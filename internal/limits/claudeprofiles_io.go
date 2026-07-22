/**
 * Resolves the configured Claude profiles from the plugin config, shared by
 * the read-side collectors (panel, sidebar, notify, activity attribution).
 */
package limits

import (
	"os"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/claude"
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
