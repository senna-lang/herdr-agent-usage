package updatecheck

import (
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ManifestFileName is the Herdr plugin manifest at the plugin root.
const ManifestFileName = "herdr-plugin.toml"

// ManifestVersion returns the version declared in the plugin manifest under
// pluginRoot, or "" when it cannot be read. The manifest is the source of truth
// for a checkout's version: locally built binaries carry no release version,
// and hooks that run the binary directly (without a wrapper script to parse
// the manifest) still need the installed version to compare against.
func ManifestVersion(pluginRoot string) string {
	if pluginRoot == "" {
		return ""
	}
	var manifest struct {
		Version string `toml:"version"`
	}
	if _, err := toml.DecodeFile(filepath.Join(pluginRoot, ManifestFileName), &manifest); err != nil {
		return ""
	}
	return manifest.Version
}
