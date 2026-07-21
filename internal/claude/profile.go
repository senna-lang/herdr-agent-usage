/**
 * Claude multi-account profile model.
 *
 * A profile is one CLAUDE_CONFIG_DIR-scoped account. All plugin-derived files
 * (limits cache, notify state, transcript root) live under the profile's config
 * dir, so two accounts never collide. Absence of any configured profile
 * synthesizes today's single implicit "claude" profile, whose derived paths are
 * byte-identical to the historical ~/.claude defaults (env overrides still win).
 */
package claude

import "path/filepath"

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
	}
}

// resolveSpec builds a concrete profile from one config entry. Env path
// overrides are deliberately ignored in multi-profile mode: a single global
// override cannot be attributed to one of several profiles.
func resolveSpec(spec ProfileSpec) ClaudeProfile {
	label := firstNonEmpty(spec.Label, spec.ID)
	jsonPath := firstNonEmpty(spec.JSONPath, filepath.Join(spec.ConfigDir, ".claude.json"))
	return ClaudeProfile{
		ID:           spec.ID,
		Label:        label,
		ConfigDir:    spec.ConfigDir,
		JSONPath:     jsonPath,
		LimitsCache:  derivedLimitsCache(spec.ConfigDir),
		StateDir:     derivedStateDir(spec.ConfigDir),
		ProjectsRoot: derivedProjectsRoot(spec.ConfigDir),
	}
}

// ResolveProfiles turns config entries into concrete profiles.
//
//   - No specs -> one synthesized default "claude" profile (backward compat).
//   - Otherwise each valid spec becomes a profile. Entries missing id or
//     config_dir are skipped, as are duplicate ids and duplicate config dirs
//     (first wins), so malformed config degrades safely rather than colliding.
func ResolveProfiles(specs []ProfileSpec, env map[string]string, home string) []ClaudeProfile {
	if len(specs) == 0 {
		return []ClaudeProfile{synthesizeDefaultProfile(env, home)}
	}
	out := make([]ClaudeProfile, 0, len(specs))
	seenID := map[string]bool{}
	seenDir := map[string]bool{}
	for _, spec := range specs {
		if spec.ID == "" || spec.ConfigDir == "" {
			continue
		}
		if seenID[spec.ID] || seenDir[spec.ConfigDir] {
			continue
		}
		seenID[spec.ID] = true
		seenDir[spec.ConfigDir] = true
		out = append(out, resolveSpec(spec))
	}
	if len(out) == 0 {
		return []ClaudeProfile{synthesizeDefaultProfile(env, home)}
	}
	return out
}

// ResolveActiveProfile picks the profile whose ConfigDir matches the given
// configDir (typically the in-process CLAUDE_CONFIG_DIR on the write side).
//
//   - Single profile: always returns it (ok=true) — the single-profile fallback.
//   - Multiple profiles: returns the config-dir match, or ok=false when none
//     match, so the caller can skip writes rather than misattribute the account.
func ResolveActiveProfile(profiles []ClaudeProfile, configDir string) (ClaudeProfile, bool) {
	if len(profiles) == 1 {
		return profiles[0], true
	}
	for _, p := range profiles {
		if p.ConfigDir == configDir {
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
