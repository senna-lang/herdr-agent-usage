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
	// Implicit is true only when zero [[claude.profiles]] were configured at
	// all (len(specs) == 0). It marks the profile that must absorb every
	// statusLine invocation regardless of CLAUDE_CONFIG_DIR, since zero-config
	// installs have no other place to attribute usage to. A profile synthesized
	// because every configured entry was invalid stays non-implicit: malformed
	// config must not silently absorb an unrelated account's usage.
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
// about the same profile. A relative config_dir is therefore rejected by
// ResolveProfiles rather than kept and compared relatively (see validateSpec).
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
//
// implicit is true only for the true zero-config case (no [[claude.profiles]]
// at all). When ResolveProfiles falls back here because every configured entry
// was invalid, implicit must be false: the caller tried to scope accounts and
// got it wrong, so ResolveActiveProfile must still require a config-dir match
// rather than absorb any CLAUDE_CONFIG_DIR into this profile's cache.
func synthesizeDefaultProfile(env map[string]string, home string, implicit bool) ClaudeProfile {
	configDir := filepath.Join(home, ".claude")
	return ClaudeProfile{
		ID:           DefaultProfileID,
		Label:        DefaultProfileLabel,
		ConfigDir:    configDir,
		JSONPath:     firstNonEmpty(env["CLAUDE_CONFIG_JSON"], filepath.Join(home, ".claude.json")),
		LimitsCache:  firstNonEmpty(env["USAGEBAR_CLAUDE_LIMITS_PATH"], derivedLimitsCache(configDir)),
		StateDir:     firstNonEmpty(env["USAGEBAR_STATE_DIR"], derivedStateDir(configDir)),
		ProjectsRoot: firstNonEmpty(env["CLAUDE_PROJECTS_ROOT"], derivedProjectsRoot(configDir)),
		Implicit:     implicit,
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

// validateSpec reports whether spec is admissible: non-empty id, and a
// config_dir that normalizes to a non-empty absolute path not already claimed
// by an earlier id or dir in this batch (seenID/seenDir are mutated by the
// caller after a true result). Shared by ResolveProfiles and
// ValidProfileSpecCount so the two can never disagree on what counts as valid.
func validateSpec(spec ProfileSpec, home string, seenID, seenDir map[string]bool) (configDir string, ok bool) {
	configDir = normalizePath(spec.ConfigDir, home)
	if spec.ID == "" || configDir == "" || !filepath.IsAbs(configDir) {
		return "", false
	}
	if seenID[spec.ID] || seenDir[configDir] {
		return "", false
	}
	return configDir, true
}

// ValidProfileSpecCount reports how many specs would survive ResolveProfiles'
// validation. Used by `usagebar setup` to detect the "every entry was invalid"
// fallback: that resolves to the same single default ClaudeProfile as a
// genuinely valid single profile re-declaring ~/.claude, so the two are
// otherwise indistinguishable by inspecting the resolved profile alone.
func ValidProfileSpecCount(specs []ProfileSpec, home string) int {
	seenID := map[string]bool{}
	seenDir := map[string]bool{}
	n := 0
	for _, spec := range specs {
		configDir, ok := validateSpec(spec, home, seenID, seenDir)
		if !ok {
			continue
		}
		seenID[spec.ID] = true
		seenDir[configDir] = true
		n++
	}
	return n
}

// ResolveProfiles turns config entries into concrete profiles.
//
//   - No specs -> one synthesized default "claude" profile (backward compat),
//     Implicit = true.
//   - Otherwise each spec passing validateSpec becomes a profile (first wins on
//     a duplicate id or dir), so malformed config degrades safely rather than
//     colliding or matching the wrong cwd. Duplicate detection compares
//     normalized dirs, so "~/.claude" and "/home/you/.claude" count as the
//     same account.
//   - If every entry was invalid, falls back to the synthesized default like
//     "no specs" -- but with Implicit = false, so a malformed config can never
//     silently absorb an unrelated account's usage (see synthesizeDefaultProfile).
func ResolveProfiles(specs []ProfileSpec, env map[string]string, home string) []ClaudeProfile {
	if len(specs) == 0 {
		return []ClaudeProfile{synthesizeDefaultProfile(env, home, true)}
	}
	out := make([]ClaudeProfile, 0, len(specs))
	seenID := map[string]bool{}
	seenDir := map[string]bool{}
	for _, spec := range specs {
		configDir, ok := validateSpec(spec, home, seenID, seenDir)
		if !ok {
			continue
		}
		seenID[spec.ID] = true
		seenDir[configDir] = true
		out = append(out, resolveSpec(spec, home))
	}
	if len(out) == 0 {
		return []ClaudeProfile{synthesizeDefaultProfile(env, home, false)}
	}
	return out
}

// ResolveActiveProfile picks the profile whose ConfigDir matches the given
// configDir (the in-process CLAUDE_CONFIG_DIR on the write side).
//
//   - Lone synthesized default from true zero-config (Implicit, no
//     [[claude.profiles]] at all): always returns it, so a relocated
//     CLAUDE_CONFIG_DIR still caches to the historical ~/.claude location both
//     sides agree on.
//   - Configured profiles, or a non-implicit synthesized default (every
//     configured entry was invalid): returns the normalized config-dir match,
//     or ok=false when none match, so the caller can skip writes rather than
//     misattribute the account. This holds for a single profile too: matching
//     one account's usage onto another is worse than not recording it.
//   - An empty configDir means Claude Code was started without the env var,
//     i.e. it is running the default account, so it resolves to ~/.claude —
//     the convention is to set CLAUDE_CONFIG_DIR only for additional accounts.
//   - A non-absolute configDir (or, in a corrupt environment, a non-absolute
//     home) never matches: relative paths compare meaninglessly against a
//     write-side cwd the read side does not share.
func ResolveActiveProfile(profiles []ClaudeProfile, configDir, home string) (ClaudeProfile, bool) {
	if len(profiles) == 1 && profiles[0].Implicit {
		return profiles[0], true
	}
	if configDir == "" {
		configDir = filepath.Join(home, ".claude")
	}
	want := normalizePath(configDir, home)
	if !filepath.IsAbs(want) {
		return ClaudeProfile{}, false
	}
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
