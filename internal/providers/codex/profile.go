/**
 * Codex multi-account profile model.
 *
 * A profile is one CODEX_HOME-scoped ChatGPT account. Each home owns its own
 * auth.json and sessions/ rollouts, so two accounts never share readings.
 * Configured paths are normalized (leading "~" expanded, cleaned) so config.toml
 * and the env var can spell the same dir differently. Absence of any configured
 * profile synthesizes today's single implicit "codex" profile at ~/.codex.
 */
package codex

import (
	"path/filepath"
	"strings"
)

// DefaultProfileID is the provider id used for the single implicit profile.
const DefaultProfileID = "codex"

// DefaultProfileLabel is the display label for the single implicit profile.
const DefaultProfileLabel = "Codex"

// ProfileSpec is one unresolved [[codex.profiles]] config entry.
type ProfileSpec struct {
	ID        string
	Label     string
	CodexHome string
}

// CodexProfile is a resolved profile with a concrete absolute home.
type CodexProfile struct {
	ID    string
	Label string
	Home  string
	// Implicit is true only when zero [[codex.profiles]] were configured at
	// all (len(specs) == 0). It marks the profile that must absorb every
	// CODEX_HOME, since zero-config installs have no other place to attribute
	// usage to. A profile synthesized because every configured entry was
	// invalid stays non-implicit: malformed config must not silently absorb
	// an unrelated account's usage.
	Implicit bool
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// normalizePath makes a configured or env-provided path comparable: it expands a
// leading "~" and cleans the result. Relative paths are deliberately NOT
// resolved against the cwd — the Codex process cwd is the user's project,
// while the plugin action cwd is unrelated.
func normalizePath(path, home string) string {
	if path == "" {
		return ""
	}
	if home != "" && (path == "~" || strings.HasPrefix(path, "~/")) {
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return filepath.Clean(path)
}

func synthesizeDefaultProfile(home string, implicit bool) CodexProfile {
	return CodexProfile{
		ID:       DefaultProfileID,
		Label:    DefaultProfileLabel,
		Home:     filepath.Join(home, ".codex"),
		Implicit: implicit,
	}
}

func resolveSpec(spec ProfileSpec, home string) CodexProfile {
	return CodexProfile{
		ID:    spec.ID,
		Label: firstNonEmpty(spec.Label, spec.ID),
		Home:  normalizePath(spec.CodexHome, home),
	}
}

func validateSpec(spec ProfileSpec, home string, seenID, seenDir map[string]bool) (codexHome string, ok bool) {
	codexHome = normalizePath(spec.CodexHome, home)
	if spec.ID == "" || codexHome == "" || !filepath.IsAbs(codexHome) {
		return "", false
	}
	if seenID[spec.ID] || seenDir[codexHome] {
		return "", false
	}
	return codexHome, true
}

// ValidProfileSpecCount reports how many specs would survive ResolveProfiles'
// validation. Used by `usagebar setup` to detect the "every entry was invalid"
// fallback.
func ValidProfileSpecCount(specs []ProfileSpec, home string) int {
	seenID := map[string]bool{}
	seenDir := map[string]bool{}
	n := 0
	for _, spec := range specs {
		codexHome, ok := validateSpec(spec, home, seenID, seenDir)
		if !ok {
			continue
		}
		seenID[spec.ID] = true
		seenDir[codexHome] = true
		n++
	}
	return n
}

// ResolveProfiles turns config entries into concrete profiles.
//
//   - No specs -> one synthesized default "codex" profile (backward compat),
//     Implicit = true.
//   - Otherwise each spec passing validateSpec becomes a profile (first wins on
//     a duplicate id or home).
//   - If every entry was invalid, falls back to the synthesized default with
//     Implicit = false, so a malformed config can never silently absorb an
//     unrelated account's usage.
func ResolveProfiles(specs []ProfileSpec, _ map[string]string, home string) []CodexProfile {
	if len(specs) == 0 {
		return []CodexProfile{synthesizeDefaultProfile(home, true)}
	}
	out := make([]CodexProfile, 0, len(specs))
	seenID := map[string]bool{}
	seenDir := map[string]bool{}
	for _, spec := range specs {
		codexHome, ok := validateSpec(spec, home, seenID, seenDir)
		if !ok {
			continue
		}
		seenID[spec.ID] = true
		seenDir[codexHome] = true
		out = append(out, resolveSpec(spec, home))
	}
	if len(out) == 0 {
		return []CodexProfile{synthesizeDefaultProfile(home, false)}
	}
	return out
}

// ResolveActiveProfile picks the profile whose Home matches the given
// CODEX_HOME (the in-process env on a Codex CLI invocation).
//
//   - Lone synthesized default from true zero-config (Implicit): always
//     returns it.
//   - Configured profiles, or a non-implicit synthesized default: returns the
//     normalized home match, or ok=false when none match.
//   - An empty home means Codex was started without CODEX_HOME, i.e. the
//     default ~/.codex account.
func ResolveActiveProfile(profiles []CodexProfile, codexHome, home string) (CodexProfile, bool) {
	if len(profiles) == 1 && profiles[0].Implicit {
		return profiles[0], true
	}
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	want := normalizePath(codexHome, home)
	if !filepath.IsAbs(want) {
		return CodexProfile{}, false
	}
	for _, p := range profiles {
		if normalizePath(p.Home, home) == want {
			return p, true
		}
	}
	return CodexProfile{}, false
}

// IsDefaultProfile reports whether p is the lone implicit default profile.
func IsDefaultProfile(p CodexProfile) bool {
	return p.ID == DefaultProfileID && p.Label == DefaultProfileLabel
}

// IsCodexProviderID reports whether a provider id belongs to any configured
// Codex profile. Replaces literal `== "codex"` checks now that a profile's id
// may be e.g. "dev".
func IsCodexProviderID(id string, profiles []CodexProfile) bool {
	for _, p := range profiles {
		if p.ID == id {
			return true
		}
	}
	return false
}
