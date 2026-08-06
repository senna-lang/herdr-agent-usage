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
	// ContextDisplay selects the sidebar context meter style:
	// "percent" (⛁ 65% (130k)) or "fraction" (⛁ 130k/200k).
	ContextDisplay string
	// ContextMaxColumns fixes the width budget for the context token.
	// 0 estimates from sidebar width and pane label, which assumes Herdr's
	// default sidebar layout; set it explicitly when custom sidebar rows put
	// $context next to a token other than the pane label.
	ContextMaxColumns int
	// ContextIconStyle selects the meter's leading glyph: "database"
	// (⛁, ⚠️ from 80%), "gauge" (▁▂▄▆█ by fill level), or "none".
	ContextIconStyle string
	// ContextLevelTokens routes the meter to $context_warm (≥60%) or
	// $context_hot (≥85%) instead of $context, so herdr sidebar rows can
	// style fill levels with different colors. Rows must then reference all
	// three tokens, which is why this is opt-in.
	ContextLevelTokens bool
	// ContextAlign "right" left-pads the meter with blank glyphs so its right
	// edge sits flush with the sidebar. Assumes a sidebar row of the form
	// [agent, $context...] (the agent display name directly before the meter).
	ContextAlign string
}

// DefaultPluginConfig is the seed default.
var DefaultPluginConfig = PluginConfig{
	RemainingThresholds: append([]int(nil), DefaultRemainingThresholds...),
	NotifyEnabled:       true,
	ContextDisplay:      "percent",
	ContextIconStyle:    "database",
	ContextAlign:        "left",
}

// pluginConfigWire mirrors the on-disk TOML shape for decoding.
type pluginConfigWire struct {
	Notify struct {
		Enabled             *bool `toml:"enabled"`
		RemainingThresholds []int `toml:"remaining_thresholds"`
	} `toml:"notify"`
	Display struct {
		ContextDisplay     string `toml:"context_display"`
		ContextMaxColumns  int    `toml:"context_max_columns"`
		ContextIconStyle   string `toml:"context_icon_style"`
		ContextLevelTokens bool   `toml:"context_level_tokens"`
		ContextAlign       string `toml:"context_align"`
	} `toml:"display"`
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
	contextDisplay := config.ContextDisplay
	if contextDisplay == "" {
		contextDisplay = DefaultPluginConfig.ContextDisplay
	}
	contextIconStyle := config.ContextIconStyle
	if contextIconStyle == "" {
		contextIconStyle = DefaultPluginConfig.ContextIconStyle
	}
	contextAlign := config.ContextAlign
	if contextAlign == "" {
		contextAlign = DefaultPluginConfig.ContextAlign
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
		"[display]",
		"# context meter style: \"percent\" (⛁ 65% (130k)) or \"fraction\" (⛁ 130k/200k)",
		"context_display = \"" + contextDisplay + "\"",
		"# fixed width budget for the context token; 0 estimates from sidebar",
		"# width and pane label (assumes Herdr's default sidebar layout)",
		"context_max_columns = " + strconv.Itoa(config.ContextMaxColumns),
		"# leading glyph: \"database\" (⛁, ⚠️ from 80%), \"gauge\" (▁▂▄▆█ by fill), \"none\"",
		"context_icon_style = \"" + contextIconStyle + "\"",
		"# route the meter to $context_warm (≥60%) / $context_hot (≥85%) so sidebar",
		"# rows can color fill levels; rows must reference all three tokens",
		"context_level_tokens = " + strconv.FormatBool(config.ContextLevelTokens),
		"# \"right\" pads the meter flush with the sidebar's right edge (assumes",
		"# a sidebar row of [agent, $context...])",
		"context_align = \"" + contextAlign + "\"",
		"",
		"# Multi-account Claude: uncomment and add one block per account.",
		"# Absence of any profile keeps the single default account (fully backward",
		"# compatible). config_dir must be unique per profile and may use ~.",
		"#",
		"# Once any profile exists, declare the default account too: bare `claude`",
		"# sets no CLAUDE_CONFIG_DIR, so the ~/.claude account is only recorded if",
		"# a profile claims that dir. `usagebar setup` warns when it is uncovered.",
		"#",
		"# [[claude.profiles]]",
		"# id = \"claude\"",
		"# label = \"Claude\"",
		"# config_dir = \"~/.claude\"",
		"# claude_json_path = \"~/.claude.json\"   # optional; defaults per config_dir",
		"#",
		"# [[claude.profiles]]",
		"# id = \"claude-secondary\"",
		"# label = \"Claude (secondary)\"",
		"# config_dir = \"~/.claude-secondary\"",
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
		ContextDisplay:      DefaultPluginConfig.ContextDisplay,
		ContextIconStyle:    DefaultPluginConfig.ContextIconStyle,
		ContextAlign:        DefaultPluginConfig.ContextAlign,
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
	if wire.Display.ContextDisplay == "percent" || wire.Display.ContextDisplay == "fraction" {
		cfg.ContextDisplay = wire.Display.ContextDisplay
	}
	if wire.Display.ContextMaxColumns >= 0 && wire.Display.ContextMaxColumns <= 200 {
		cfg.ContextMaxColumns = wire.Display.ContextMaxColumns
	}
	if wire.Display.ContextIconStyle == "database" || wire.Display.ContextIconStyle == "gauge" || wire.Display.ContextIconStyle == "none" {
		cfg.ContextIconStyle = wire.Display.ContextIconStyle
	}
	cfg.ContextLevelTokens = wire.Display.ContextLevelTokens
	if wire.Display.ContextAlign == "left" || wire.Display.ContextAlign == "right" {
		cfg.ContextAlign = wire.Display.ContextAlign
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

// ResolveActiveClaudeProfile resolves the configured profiles and picks the one
// this process is running as, using its own CLAUDE_CONFIG_DIR. Write side only
// (statusLine): the read side cannot see that env var. The full profile list is
// returned alongside so a caller can report an unmatched config dir.
func ResolveActiveClaudeProfile(env map[string]string) (claude.ClaudeProfile, []claude.ClaudeProfile, bool) {
	profiles := ResolveClaudeProfiles(env)
	home, _ := os.UserHomeDir()
	profile, ok := claude.ResolveActiveProfile(profiles, env["CLAUDE_CONFIG_DIR"], home)
	return profile, profiles, ok
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
