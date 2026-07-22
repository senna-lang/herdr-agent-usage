/**
 * Seeds and loads the plugin-specific config (HERDR_PLUGIN_CONFIG_DIR).
 * Lives in a separate space from the main Herdr config.toml.
 *
 * Parsing uses a real TOML decoder (BurntSushi) so array-of-tables
 * ([[claude.profiles]]) is handled correctly; the seed body is still authored as
 * a string for readable inline comments.
 */
package setup

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/senna-lang/herdr-agent-usage/internal/claude"
)

// DefaultRemainingThresholds are the toast remaining-% buckets.
var DefaultRemainingThresholds = []int{50, 20, 10, 5}

// PluginConfig is the plugin-local config shape.
type PluginConfig struct {
	RemainingThresholds []int
	// NotifyEnabled is the plugin-side intent (separate from host toast delivery).
	NotifyEnabled bool
	// ClaudeProfiles are the configured [[claude.profiles]] entries (unresolved).
	// Empty means the single implicit "claude" profile is synthesized downstream.
	ClaudeProfiles []claude.ProfileSpec
}

// DefaultPluginConfig is the seed default.
var DefaultPluginConfig = PluginConfig{
	RemainingThresholds: append([]int(nil), DefaultRemainingThresholds...),
	NotifyEnabled:       true,
}

// pluginConfigWire mirrors the on-disk TOML shape for decoding.
type pluginConfigWire struct {
	Notify struct {
		Enabled             *bool `toml:"enabled"`
		RemainingThresholds []int `toml:"remaining_thresholds"`
	} `toml:"notify"`
	Claude struct {
		Profiles []profileWire `toml:"profiles"`
	} `toml:"claude"`
}

type profileWire struct {
	ID             string `toml:"id"`
	Label          string `toml:"label"`
	ConfigDir      string `toml:"config_dir"`
	ClaudeJSONPath string `toml:"claude_json_path"`
}

// ResolvePluginConfigDir resolves the config directory.
// HERDR_PLUGIN_CONFIG_DIR → else ~/.config/herdr/plugins/config/usagebar
func ResolvePluginConfigDir(env map[string]string) string {
	if fromEnv := env["HERDR_PLUGIN_CONFIG_DIR"]; fromEnv != "" {
		return fromEnv
	}
	if xdg := env["XDG_CONFIG_HOME"]; xdg != "" {
		return filepath.Join(xdg, "herdr", "plugins", "config", "usagebar")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "herdr", "plugins", "config", "usagebar")
}

// PluginConfigPath is config.toml under the plugin config dir.
func PluginConfigPath(configDir string) string {
	return filepath.Join(configDir, "config.toml")
}

// DefaultPluginConfigTOML is the default config.toml body.
func DefaultPluginConfigTOML(config PluginConfig) string {
	if len(config.RemainingThresholds) == 0 {
		config = DefaultPluginConfig
	}
	parts := make([]string, len(config.RemainingThresholds))
	for i, n := range config.RemainingThresholds {
		parts[i] = strconv.Itoa(n)
	}
	thresholds := strings.Join(parts, ", ")
	enabled := "false"
	if config.NotifyEnabled {
		enabled = "true"
	}
	return strings.Join([]string{
		"# Agent Usage (usagebar) plugin config",
		"# Path: herdr plugin config-dir usagebar",
		"",
		"[notify]",
		"enabled = " + enabled,
		"# remaining % thresholds that may fire a toast (once per window/bucket)",
		"remaining_thresholds = [" + thresholds + "]",
		"",
		"# Multi-account Claude: uncomment and add one block per CLAUDE_CONFIG_DIR.",
		"# Absence of any profile keeps the single default account (fully backward",
		"# compatible). config_dir must be unique per profile.",
		"#",
		"# [[claude.profiles]]",
		"# id = \"claude\"",
		"# label = \"Claude\"",
		"# config_dir = \"/home/you/.claude\"",
		"# claude_json_path = \"/home/you/.claude.json\"",
		"#",
		"# [[claude.profiles]]",
		"# id = \"claude-secondary\"",
		"# label = \"Claude (secondary)\"",
		"# config_dir = \"/home/you/.claude-secondary\"",
		"",
	}, "\n")
}

// validThresholds keeps the historical rule: every value must be 1..100, else
// the whole set is rejected in favor of the default.
func validThresholds(in []int) ([]int, bool) {
	if len(in) == 0 {
		return nil, false
	}
	out := make([]int, 0, len(in))
	for _, n := range in {
		if n <= 0 || n > 100 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// ParsePluginConfigTOML decodes the plugin config, falling back to defaults for
// missing/invalid fields (malformed TOML yields all defaults).
func ParsePluginConfigTOML(raw string) PluginConfig {
	cfg := PluginConfig{
		NotifyEnabled:       DefaultPluginConfig.NotifyEnabled,
		RemainingThresholds: append([]int(nil), DefaultPluginConfig.RemainingThresholds...),
	}
	var wire pluginConfigWire
	if _, err := toml.Decode(raw, &wire); err != nil {
		return cfg
	}
	if wire.Notify.Enabled != nil {
		cfg.NotifyEnabled = *wire.Notify.Enabled
	}
	if thr, ok := validThresholds(wire.Notify.RemainingThresholds); ok {
		cfg.RemainingThresholds = thr
	}
	for _, p := range wire.Claude.Profiles {
		cfg.ClaudeProfiles = append(cfg.ClaudeProfiles, claude.ProfileSpec{
			ID:        p.ID,
			Label:     p.Label,
			ConfigDir: p.ConfigDir,
			JSONPath:  p.ClaudeJSONPath,
		})
	}
	return cfg
}

// SeedPluginConfigIfMissing writes default config.toml when missing; returns true if created.
func SeedPluginConfigIfMissing(configDir string) bool {
	_ = os.MkdirAll(configDir, 0o755)
	path := PluginConfigPath(configDir)
	if _, err := os.Stat(path); err == nil {
		return false
	}
	_ = os.WriteFile(path, []byte(DefaultPluginConfigTOML(DefaultPluginConfig)), 0o644)
	return true
}

// ResolveClaudeProfiles loads the plugin config and resolves its
// [[claude.profiles]] into concrete profiles (synthesizing the single implicit
// default when none are configured). Shared by the write side (statusLine
// routing) and the read side (panel/sidebar/notify).
func ResolveClaudeProfiles(env map[string]string) []claude.ClaudeProfile {
	cfg := LoadPluginConfig(ResolvePluginConfigDir(env))
	home, _ := os.UserHomeDir()
	return claude.ResolveProfiles(cfg.ClaudeProfiles, env, home)
}

// LoadPluginConfig loads config.toml or returns defaults.
func LoadPluginConfig(configDir string) PluginConfig {
	path := PluginConfigPath(configDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		return DefaultPluginConfig
	}
	return ParsePluginConfigTOML(string(raw))
}
