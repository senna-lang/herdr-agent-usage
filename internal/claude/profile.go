/**
 * Claude multi-account profile model.
 *
 * A profile is one CLAUDE_CONFIG_DIR-scoped account. All plugin-derived files
 * (limits cache, notify state, transcript root) live under the profile's config
 * dir, so two accounts never collide. Configured paths are normalized (leading
 * "~" expanded, cleaned) so config.toml and the env var can spell the same dir
 * differently. Absence of any configured profile synthesizes today's single
 * implicit "claude" profile, whose derived paths are byte-identical to the
 * historical ~/.claude defaults (env overrides still win).
 */
package claude

import (
	"path/filepath"
	"strings"
)

// DefaultProfileID is the provider id used for the single implicit profile.
const DefaultProfileID = "claude"

// DefaultProfileLabel is the display label for the single implicit profile.
const DefaultProfileLabel = "Claude"

// ProfileSpec is one unresolved [[claude.profiles]] config entry.
type ProfileSpec struct {
	ID        string
	Label     string
	ConfigDir string
	JSONPath  string
}

// ClaudeProfile is a resolved profile with concrete absolute paths.
type ClaudeProfile struct {
	ID           string
	Label        string
	ConfigDir    string
	JSONPath     string // .claude.json for this profile
	LimitsCache  string // statusLine limits cache
	StateDir     string // notify state + lock dir
	ProjectsRoot string // transcript projects root
	// Implicit is true only for the synthesized default profile (no
	// [[claude.profiles]] configured at all). It marks the profile that must
	// absorb every statusLine invocation regardless of CLAUDE_CONFIG_DIR, since
	// zero-config installs have no other place to attribute usage to.
	Implicit bool
}

// derivedLimitsCache is the per-config-dir limits cache path.
func derivedLimitsCache(configDir string) string {
	return filepath.Join(configDir, "herdr-usagebar", "claude-limits-latest.json")
}

// derivedStateDir is the per-config-dir notify state dir.
func derivedStateDir(configDir string) string {
	return filepath.Join(configDir, "herdr-usagebar")
}

// derivedProjectsRoot is the per-config-dir transcript projects root.
func derivedProjectsRoot(configDir string) string {
	return filepath.Join(configDir, "projects")
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// normalizePath makes a configured or env-provided path comparable: it expands a
// leading "~" (nothing expands it when the value comes from TOML) and cleans the
// result, so "~/.claude", "/home/u/.claude/" and "/home/u/./.claude" all reduce
// to the same string.
//
// Relative paths are deliberately NOT resolved against the cwd. The write side
// (statusLine) runs inside the Claude process with cwd = the user's project,
// while the read side (panel/sidebar/notify) runs as a Herdr plugin action with
// an unrelated cwd; expanding against cwd would make the two sides disagree
// about the same profile. A relative config_dir therefore stays relative and
// simply fails to match, which the caller reports.
func normalizePath(path, home string) string {
	if path == "" {
		return ""
	}
	if home != "" && (path == "~" || strings.HasPrefix(path, "~/")) {
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return filepath.Clean(path)
}

// synthesizeDefaultProfile builds the single implicit "claude" profile.
//
// Its derived paths are anchored to the historical ~/.claude location, NOT
// CLAUDE_CONFIG_DIR: the write side (statusLine, in-process) can see that env
// var but the read side (panel/sidebar/notify, a Herdr plugin action) cannot,
// so deriving the default off it would make the two sides disagree on where an
// unconfigured account's files live. Isolation for a relocated CLAUDE_CONFIG_DIR
// is opt-in via an explicit [[claude.profiles]] entry (config_dir is visible to
// both sides because it comes from config, not env).
//
// The explicit USAGEBAR_*/CLAUDE_PROJECTS_ROOT/CLAUDE_CONFIG_JSON overrides
// still apply here since they are pre-existing, side-agnostic escape hatches
// the caller sets deliberately (not an implicit CLAUDE_CONFIG_DIR read).
func synthesizeDefaultProfile(env map[string]string, home string) ClaudeProfile {
	configDir := filepath.Join(home, ".claude")
	return ClaudeProfile{
		ID:           DefaultProfileID,
		Label:        DefaultProfileLabel,
		ConfigDir:    configDir,
		JSONPath:     firstNonEmpty(env["CLAUDE_CONFIG_JSON"], filepath.Join(home, ".claude.json")),
		LimitsCache:  firstNonEmpty(env["USAGEBAR_CLAUDE_LIMITS_PATH"], derivedLimitsCache(configDir)),
		StateDir:     firstNonEmpty(env["USAGEBAR_STATE_DIR"], derivedStateDir(configDir)),
		ProjectsRoot: firstNonEmpty(env["CLAUDE_PROJECTS_ROOT"], derivedProjectsRoot(configDir)),
		Implicit:     true,
	}
}

// defaultJSONPathFor returns the default .claude.json path for configDir.
//
// Claude Code does not relocate .claude.json under CLAUDE_CONFIG_DIR — it
// always lives at the fixed sibling path ~/.claude.json regardless of which
// config dir is active, unless the user separately sets CLAUDE_CONFIG_JSON.
// So a profile whose config_dir happens to equal the real default (~/.claude,
// e.g. when re-declaring the default account alongside additional accounts,
// as the multi-profile config example does) must default its JSONPath to
// that same sibling file, not <config_dir>/.claude.json — the naive pattern
// that holds for any other, genuinely separate config dir.
func defaultJSONPathFor(configDir, home string) string {
	if configDir == filepath.Join(home, ".claude") {
		return filepath.Join(home, ".claude.json")
	}
	return filepath.Join(configDir, ".claude.json")
}

// resolveSpec builds a concrete profile from one config entry. Env path
// overrides are deliberately ignored in multi-profile mode: a single global
// override cannot be attributed to one of several profiles.
//
// Paths are normalized (leading "~" expanded, cleaned) before anything is
// derived from them: a literal "~/.claude-dev" would otherwise become a
// derived cache under a directory actually named "~".
func resolveSpec(spec ProfileSpec, home string) ClaudeProfile {
	label := firstNonEmpty(spec.Label, spec.ID)
	configDir := normalizePath(spec.ConfigDir, home)
	jsonPath := firstNonEmpty(normalizePath(spec.JSONPath, home), defaultJSONPathFor(configDir, home))
	return ClaudeProfile{
		ID:           spec.ID,
		Label:        label,
		ConfigDir:    configDir,
		JSONPath:     jsonPath,
		LimitsCache:  derivedLimitsCache(configDir),
		StateDir:     derivedStateDir(configDir),
		ProjectsRoot: derivedProjectsRoot(configDir),
	}
}

// ResolveProfiles turns config entries into concrete profiles.
//
//   - No specs -> one synthesized default "claude" profile (backward compat).
//   - Otherwise each valid spec becomes a profile. Entries missing id or
//     config_dir are skipped, as are duplicate ids and duplicate config dirs
//     (first wins), so malformed config degrades safely rather than colliding.
//     Duplicate detection compares normalized dirs, so "~/.claude" and
//     "/home/you/.claude" count as the same account.
func ResolveProfiles(specs []ProfileSpec, env map[string]string, home string) []ClaudeProfile {
	if len(specs) == 0 {
		return []ClaudeProfile{synthesizeDefaultProfile(env, home)}
	}
	out := make([]ClaudeProfile, 0, len(specs))
	seenID := map[string]bool{}
	seenDir := map[string]bool{}
	for _, spec := range specs {
		configDir := normalizePath(spec.ConfigDir, home)
		if spec.ID == "" || configDir == "" {
			continue
		}
		if seenID[spec.ID] || seenDir[configDir] {
			continue
		}
		seenID[spec.ID] = true
		seenDir[configDir] = true
		out = append(out, resolveSpec(spec, home))
	}
	if len(out) == 0 {
		return []ClaudeProfile{synthesizeDefaultProfile(env, home)}
	}
	return out
}

// ResolveActiveProfile picks the profile whose ConfigDir matches the given
// configDir (the in-process CLAUDE_CONFIG_DIR on the write side).
//
//   - Lone synthesized default (no [[claude.profiles]] at all): always returns
//     it, so a zero-config install with a relocated CLAUDE_CONFIG_DIR keeps
//     caching to the historical ~/.claude location both sides agree on.
//   - Configured profiles: returns the normalized config-dir match, or ok=false
//     when none match, so the caller can skip writes rather than misattribute
//     the account. This holds for a single configured profile too: matching one
//     account's usage onto another profile is worse than not recording it.
//   - An empty configDir means Claude Code was started without the env var,
//     i.e. it is running the default account, so it resolves to ~/.claude —
//     the convention is to set CLAUDE_CONFIG_DIR only for additional accounts.
func ResolveActiveProfile(profiles []ClaudeProfile, configDir, home string) (ClaudeProfile, bool) {
	if len(profiles) == 1 && profiles[0].Implicit {
		return profiles[0], true
	}
	if configDir == "" {
		configDir = filepath.Join(home, ".claude")
	}
	want := normalizePath(configDir, home)
	for _, p := range profiles {
		if normalizePath(p.ConfigDir, home) == want {
			return p, true
		}
	}
	return ClaudeProfile{}, false
}

// IsDefaultProfile reports whether p is the lone implicit default profile,
// used to decide whether notification titles get a label prefix.
func IsDefaultProfile(p ClaudeProfile) bool {
	return p.ID == DefaultProfileID && p.Label == DefaultProfileLabel
}

// IsClaudeProviderID reports whether a provider id belongs to any configured
// Claude profile. Replaces literal `== "claude"` checks now that a profile's id
// may be e.g. "claude-secondary".
func IsClaudeProviderID(id string, profiles []ClaudeProfile) bool {
	for _, p := range profiles {
		if p.ID == id {
			return true
		}
	}
	return false
}
