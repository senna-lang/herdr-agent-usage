/** OpenCode profile configuration binds one provider id to one data directory. */
package opencode

import (
	"path/filepath"
	"strings"
)

const (
	// DefaultProfileID identifies the zero-configuration OpenCode account.
	DefaultProfileID = "opencode"
	// DefaultProfileLabel is the default display label.
	DefaultProfileLabel = "OpenCode"
)

// ProfileSpec is one [[opencode.profiles]] configuration entry.
type ProfileSpec struct {
	ID      string
	Label   string
	DataDir string
}

// OpenCodeProfile is a validated profile bound to one data directory.
type OpenCodeProfile struct {
	ID       string
	Label    string
	DataDir  string
	Implicit bool
}

func normalizeProfileDataDir(path, home string) string {
	if home != "" && (path == "~" || strings.HasPrefix(path, "~/")) {
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func defaultDataDir(env map[string]string, home string) string {
	if xdg := env["XDG_DATA_HOME"]; xdg != "" {
		return filepath.Join(xdg, "opencode")
	}
	return filepath.Join(home, ".local", "share", "opencode")
}

// ResolveProfiles validates distinct absolute OPENCODE_DATA_DIR entries.
func ResolveProfiles(specs []ProfileSpec, env map[string]string, home string) []OpenCodeProfile {
	defaultProfile := func(implicit bool) OpenCodeProfile {
		return OpenCodeProfile{
			ID:       DefaultProfileID,
			Label:    DefaultProfileLabel,
			DataDir:  defaultDataDir(env, home),
			Implicit: implicit,
		}
	}
	if len(specs) == 0 {
		return []OpenCodeProfile{defaultProfile(true)}
	}

	profiles := make([]OpenCodeProfile, 0, len(specs))
	ids := make(map[string]bool)
	dirs := make(map[string]bool)
	for _, spec := range specs {
		dataDir := normalizeProfileDataDir(spec.DataDir, home)
		if spec.ID == "" || dataDir == "" || !filepath.IsAbs(dataDir) || ids[spec.ID] || dirs[dataDir] {
			continue
		}

		label := spec.Label
		if label == "" {
			label = spec.ID
		}
		ids[spec.ID] = true
		dirs[dataDir] = true
		profiles = append(profiles, OpenCodeProfile{
			ID:      spec.ID,
			Label:   label,
			DataDir: dataDir,
		})
	}
	if len(profiles) == 0 {
		return []OpenCodeProfile{defaultProfile(false)}
	}
	return profiles
}

// ValidProfileSpecCount reports how many specs resolve to distinct account
// data directories. Setup uses it to distinguish malformed config from none.
func ValidProfileSpecCount(specs []ProfileSpec, home string) int {
	ids := make(map[string]bool)
	dirs := make(map[string]bool)
	count := 0
	for _, spec := range specs {
		dataDir := normalizeProfileDataDir(spec.DataDir, home)
		if len(spec.ID) == 0 || len(dataDir) == 0 || !filepath.IsAbs(dataDir) || ids[spec.ID] || dirs[dataDir] {
			continue
		}
		ids[spec.ID] = true
		dirs[dataDir] = true
		count++
	}
	return count
}
