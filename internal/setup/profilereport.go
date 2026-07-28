/**
 * Renders the [[claude.profiles]] block of `usagebar setup`.
 *
 * Profile misconfiguration is silent at runtime: the status line still shows
 * correct numbers while nothing is cached for the account, so setup is the only
 * place a user can see that an entry was dropped, that a config_dir cannot be
 * resolved, or that the default account (bare `claude`, no CLAUDE_CONFIG_DIR)
 * is not covered by any profile.
 */
package setup

import (
	"path/filepath"

	"github.com/senna-lang/herdr-agent-usage/internal/claude"
)

// claudeProfileReportLines renders the configured profiles and the warnings for
// config that silently loses usage data. specs are the raw config entries,
// profiles the result of resolving them, home the user's home dir.
func claudeProfileReportLines(specs []claude.ProfileSpec, profiles []claude.ClaudeProfile, home string) []string {
	if len(profiles) == 1 && profiles[0].Implicit {
		lines := []string{"· claude profiles: none configured; single default account: " + profiles[0].ConfigDir}
		if len(specs) > 0 {
			lines = append(lines, "  ! all "+itoa(len(specs))+" [[claude.profiles]] entries were ignored:"+
				" each needs an id and a unique config_dir")
		}
		return append(lines, "")
	}

	defaultDir := filepath.Join(home, ".claude")
	lines := []string{"· claude profiles: " + itoa(len(profiles)) + " configured"}
	coversDefault := false
	for _, p := range profiles {
		row := "    " + p.ID + "  " + p.ConfigDir
		if p.ConfigDir == defaultDir {
			coversDefault = true
			row += "  (default account)"
		}
		lines = append(lines, row)
	}
	if dropped := len(specs) - len(profiles); dropped > 0 {
		lines = append(lines, "  ! "+itoa(dropped)+" entr"+plural(dropped, "y", "ies")+
			" ignored: each needs an id and a unique config_dir")
	}
	for _, p := range profiles {
		if !filepath.IsAbs(p.ConfigDir) {
			lines = append(lines, "  ! "+p.ID+": config_dir is not absolute ("+p.ConfigDir+
				"); use an absolute path or ~/...")
		}
	}
	if !coversDefault {
		lines = append(lines, "  ! no profile has config_dir = "+defaultDir+
			"; usage of the account started by bare `claude` (no CLAUDE_CONFIG_DIR) is not recorded")
	}
	return append(lines, "")
}

// plural picks the singular or plural suffix for n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
