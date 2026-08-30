/**
 * Seeds and loads the plugin-specific config (HERDR_PLUGIN_CONFIG_DIR).
 * Lives in a separate space from the main Herdr config.toml.
 *
 * Parsing uses a real TOML decoder (BurntSushi) so array-of-tables
 * ([[claude.profiles]], [[codex.profiles]]) is handled correctly; the seed body
 * is still authored as a string for readable inline comments.
 */
package setup

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
)

// DefaultRemainingThresholds are the toast remaining-% buckets.
var DefaultRemainingThresholds = []int{50, 20, 10, 5}

// PluginConfig is the plugin-local config shape.
type PluginConfig struct {
	RemainingThresholds []int
	// NotifyEnabled is the plugin-side intent (separate from host toast delivery).
	NotifyEnabled bool
	// LimitPercent is the presentation direction for quota percentages.
	// remaining (default) shows headroom; used shows consumption. Notify
	// firing still uses remaining thresholds.
	LimitPercent core.LimitPercent
	// ClaudeProfiles are the configured [[claude.profiles]] entries (unresolved).
	// Empty means the single implicit "claude" profile is synthesized downstream.
	ClaudeProfiles []claude.ProfileSpec
	// CodexProfiles are the configured [[codex.profiles]] entries (unresolved).
	// Empty means the single implicit "codex" profile is synthesized downstream.
	CodexProfiles    []codex.ProfileSpec
	GrokProfiles     []grok.ProfileSpec
	OpenCodeProfiles []opencode.ProfileSpec
}

// DefaultPluginConfig is the seed default.
var DefaultPluginConfig = PluginConfig{
	RemainingThresholds: append([]int(nil), DefaultRemainingThresholds...),
	NotifyEnabled:       true,
	LimitPercent:        core.LimitPercentRemaining,
}

// pluginConfigWire mirrors the on-disk TOML shape for decoding.
type pluginConfigWire struct {
	Notify struct {
		Enabled             *bool `toml:"enabled"`
		RemainingThresholds []int `toml:"remaining_thresholds"`
	} `toml:"notify"`
	UI struct {
		LimitPercent *string `toml:"limit_percent"`
	} `toml:"ui"`
	Claude struct {
		Profiles []profileWire `toml:"profiles"`
	} `toml:"claude"`
	Codex struct {
		Profiles []codexProfileWire `toml:"profiles"`
	} `toml:"codex"`

	Grok struct {
		Profiles []grokProfileWire `toml:"profiles"`
	} `toml:"grok"`
	OpenCode struct {
		Profiles []openCodeProfileWire `toml:"profiles"`
	} `toml:"opencode"`
}

type profileWire struct {
	ID             string `toml:"id"`
	Label          string `toml:"label"`
	ConfigDir      string `toml:"config_dir"`
	ClaudeJSONPath string `toml:"claude_json_path"`
}

type codexProfileWire struct {
	ID        string `toml:"id"`
	Label     string `toml:"label"`
	CodexHome string `toml:"codex_home"`
}

type grokProfileWire struct {
	ID       string `toml:"id"`
	Label    string `toml:"label"`
	GrokHome string `toml:"grok_home"`
}

type openCodeProfileWire struct {
	ID      string `toml:"id"`
	Label   string `toml:"label"`
	DataDir string `toml:"data_dir"`
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
		"[ui]",
		"# remaining (default): % left / higher is safer. used: fill as you burn.",
		`limit_percent = "` + string(core.ParseLimitPercent(string(config.LimitPercent))) + `"`,
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
		"# Multi-account Codex: uncomment and add one block per CODEX_HOME.",
		"# Absence of any profile keeps the single default account at ~/.codex.",
		"# Once any profile exists, declare the default account too: bare `codex`",
		"# sets no CODEX_HOME, so ~/.codex is only recorded if a profile claims",
		"# that dir. `usagebar setup` warns when it is uncovered.",
		"#",
		"# [[codex.profiles]]",
		"# id = \"codex\"",
		"# label = \"personal\"",
		"# codex_home = \"~/.codex\"",
		"#",
		"# [[codex.profiles]]",
		"# id = \"dev\"",
		"# label = \"product\"",
		"# codex_home = \"~/.codex-dev\"",
		"",
		"# Multi-account Grok: one GROK_HOME per profile.",
		"# [[grok.profiles]]",
		"# id = \"grok\"",
		"# label = \"personal\"",
		"# grok_home = \"~/.grok\"",
		"#",
		"# Multi-account OpenCode: one OPENCODE_DATA_DIR per profile.",
		"# [[opencode.profiles]]",
		"# id = \"opencode\"",
		"# label = \"personal\"",
		"# data_dir = \"~/.local/share/opencode\"",
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
		LimitPercent:        DefaultPluginConfig.LimitPercent,
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
	if wire.UI.LimitPercent != nil {
		cfg.LimitPercent = core.ParseLimitPercent(*wire.UI.LimitPercent)
	}

	for _, p := range wire.Claude.Profiles {
		cfg.ClaudeProfiles = append(cfg.ClaudeProfiles, claude.ProfileSpec{
			ID:        p.ID,
			Label:     p.Label,
			ConfigDir: p.ConfigDir,
			JSONPath:  p.ClaudeJSONPath,
		})
	}
	for _, p := range wire.Codex.Profiles {
		cfg.CodexProfiles = append(cfg.CodexProfiles, codex.ProfileSpec{
			ID:        p.ID,
			Label:     p.Label,
			CodexHome: p.CodexHome,
		})
	}
	for _, p := range wire.Grok.Profiles {
		cfg.GrokProfiles = append(cfg.GrokProfiles, grok.ProfileSpec{ID: p.ID, Label: p.Label, GrokHome: p.GrokHome})
	}
	for _, p := range wire.OpenCode.Profiles {
		cfg.OpenCodeProfiles = append(cfg.OpenCodeProfiles, opencode.ProfileSpec{ID: p.ID, Label: p.Label, DataDir: p.DataDir})
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

// ResolveCodexProfiles loads the plugin config and resolves its
// [[codex.profiles]] into concrete profiles (synthesizing the single implicit
// default when none are configured).
func ResolveCodexProfiles(env map[string]string) []codex.CodexProfile {
	cfg := LoadPluginConfig(ResolvePluginConfigDir(env))
	home, _ := os.UserHomeDir()
	return codex.ResolveProfiles(cfg.CodexProfiles, env, home)
}

// ResolveActiveCodexProfile picks the profile matching this process's
// CODEX_HOME. Empty CODEX_HOME means the default ~/.codex account.
func ResolveActiveCodexProfile(env map[string]string) (codex.CodexProfile, []codex.CodexProfile, bool) {
	profiles := ResolveCodexProfiles(env)
	home, _ := os.UserHomeDir()
	profile, ok := codex.ResolveActiveProfile(profiles, env["CODEX_HOME"], home)
	return profile, profiles, ok
}

func ResolveGrokProfiles(env map[string]string) []grok.GrokProfile {
	cfg := LoadPluginConfig(ResolvePluginConfigDir(env))
	home, _ := os.UserHomeDir()
	return grok.ResolveProfiles(cfg.GrokProfiles, env, home)
}

func ResolveOpenCodeProfiles(env map[string]string) []opencode.OpenCodeProfile {
	cfg := LoadPluginConfig(ResolvePluginConfigDir(env))
	home, _ := os.UserHomeDir()
	return opencode.ResolveProfiles(cfg.OpenCodeProfiles, env, home)
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
