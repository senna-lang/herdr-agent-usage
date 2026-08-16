/** Grok profile configuration binds one provider id to one GROK_HOME. */
package grok

import (
	"path/filepath"
	"strings"
)

const (
	// DefaultProfileID identifies the zero-configuration Grok account.
	DefaultProfileID = "grok"
	// DefaultProfileLabel is the default display label.
	DefaultProfileLabel = "Grok"
)

// ProfileSpec is one [[grok.profiles]] configuration entry.
type ProfileSpec struct {
	ID       string
	Label    string
	GrokHome string
}

// GrokProfile is a validated profile bound to exactly one GROK_HOME.
type GrokProfile struct {
	ID       string
	Label    string
	Home     string
	Implicit bool
}

func normalizeProfileHome(path, home string) string {
	if home != "" && (path == "~" || strings.HasPrefix(path, "~/")) {
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

// ResolveProfiles validates unique absolute homes. Malformed explicit config
// retains the default location without treating it as an attribution catch-all.
func ResolveProfiles(specs []ProfileSpec, _ map[string]string, home string) []GrokProfile {
	defaultProfile := func(implicit bool) GrokProfile {
		return GrokProfile{
			ID:       DefaultProfileID,
			Label:    DefaultProfileLabel,
			Home:     filepath.Join(home, ".grok"),
			Implicit: implicit,
		}
	}
	if len(specs) == 0 {
		return []GrokProfile{defaultProfile(true)}
	}

	profiles := make([]GrokProfile, 0, len(specs))
	ids := make(map[string]bool)
	homes := make(map[string]bool)
	for _, spec := range specs {
		profileHome := normalizeProfileHome(spec.GrokHome, home)
		if spec.ID == "" || profileHome == "" || !filepath.IsAbs(profileHome) || ids[spec.ID] || homes[profileHome] {
			continue
		}

		label := spec.Label
		if label == "" {
			label = spec.ID
		}
		ids[spec.ID] = true
		homes[profileHome] = true
		profiles = append(profiles, GrokProfile{
			ID:    spec.ID,
			Label: label,
			Home:  profileHome,
		})
	}
	if len(profiles) == 0 {
		return []GrokProfile{defaultProfile(false)}
	}
	return profiles
}

// ValidProfileSpecCount reports how many specs resolve to distinct account
// homes. Setup uses it to distinguish malformed configuration from no config.
func ValidProfileSpecCount(specs []ProfileSpec, home string) int {
	ids := make(map[string]bool)
	homes := make(map[string]bool)
	count := 0
	for _, spec := range specs {
		profileHome := normalizeProfileHome(spec.GrokHome, home)
		if spec.ID == "" || profileHome == "" || !filepath.IsAbs(profileHome) || ids[spec.ID] || homes[profileHome] {
			continue
		}
		ids[spec.ID] = true
		homes[profileHome] = true
		count++
	}
	return count
}
