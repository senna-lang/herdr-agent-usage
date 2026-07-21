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

// claudeProfileByID looks up one resolved profile by provider id.
func claudeProfileByID(id string) (claude.ClaudeProfile, bool) {
	for _, p := range ResolvedClaudeProfiles() {
		if p.ID == id {
			return p, true
		}
	}
	return claude.ClaudeProfile{}, false
}
