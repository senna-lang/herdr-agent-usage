/**
 * Renders the [[claude.profiles]] and [[codex.profiles]] blocks of `usagebar setup`.
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

	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
)

// claudeProfileReportLines renders the configured profiles and the warnings for
// config that silently loses usage data. specs are the raw config entries,
// profiles the result of resolving them, home the user's home dir.
//
// The "every entry was invalid" fallback and a genuinely valid single profile
// re-declaring ~/.claude resolve to the same ClaudeProfile, so the branch below
// checks claude.ValidProfileSpecCount rather than inspecting profiles: it is
// the only signal that distinguishes "no real config" from "config, but wrong".
func claudeProfileReportLines(specs []claude.ProfileSpec, profiles []claude.ClaudeProfile, home string) []string {
	validCount := claude.ValidProfileSpecCount(specs, home)

	if len(specs) == 0 {
		return []string{
			"· claude profiles: none configured; single default account: " + profiles[0].ConfigDir,
			"",
		}
	}
	if validCount == 0 {
		return []string{
			"· claude profiles: none configured; single default account: " + profiles[0].ConfigDir,
			"  ! all " + itoa(len(specs)) + " [[claude.profiles]] entries were ignored:" +
				" each needs an id and a unique, absolute config_dir",
			"",
		}
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
	if dropped := len(specs) - validCount; dropped > 0 {
		lines = append(lines, "  ! "+itoa(dropped)+" entr"+plural(dropped, "y", "ies")+
			" ignored: each needs an id and a unique, absolute config_dir")
	}
	if !coversDefault {
		lines = append(lines, "  ! no profile has config_dir = "+defaultDir+
			"; usage of the account started by bare `claude` (no CLAUDE_CONFIG_DIR) is not recorded")
	}
	return append(lines, "")
}

// codexProfileReportLines renders the configured Codex profiles and the warnings
// for config that silently loses usage data. Same shape as the Claude report:
// ValidProfileSpecCount distinguishes "no real config" from "config, but wrong".
func codexProfileReportLines(specs []codex.ProfileSpec, profiles []codex.CodexProfile, home string) []string {
	validCount := codex.ValidProfileSpecCount(specs, home)

	if len(specs) == 0 {
		return []string{
			"· codex profiles: none configured; single default account: " + profiles[0].Home,
			"",
		}
	}
	if validCount == 0 {
		return []string{
			"· codex profiles: none configured; single default account: " + profiles[0].Home,
			"  ! all " + itoa(len(specs)) + " [[codex.profiles]] entries were ignored:" +
				" each needs an id and a unique, absolute codex_home",
			"",
		}
	}

	defaultDir := filepath.Join(home, ".codex")
	lines := []string{"· codex profiles: " + itoa(len(profiles)) + " configured"}
	coversDefault := false
	for _, p := range profiles {
		row := "    " + p.ID + "  " + p.Home
		if p.Home == defaultDir {
			coversDefault = true
			row += "  (default account)"
		}
		lines = append(lines, row)
	}
	if dropped := len(specs) - validCount; dropped > 0 {
		lines = append(lines, "  ! "+itoa(dropped)+" entr"+plural(dropped, "y", "ies")+
			" ignored: each needs an id and a unique, absolute codex_home")
	}
	if !coversDefault {
		lines = append(lines, "  ! no profile has codex_home = "+defaultDir+
			"; usage of the account started by bare `codex` (no CODEX_HOME) is not recorded")
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
