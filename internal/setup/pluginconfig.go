/**
 * プラグイン専用 config (HERDR_PLUGIN_CONFIG_DIR) の seed / 読み込み。
 * Herdr 本体の config.toml とは別空間。
 */
package setup

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DefaultRemainingThresholds are the toast remaining-% buckets.
var DefaultRemainingThresholds = []int{50, 20, 10, 5}

// PluginConfig is the plugin-local config shape.
type PluginConfig struct {
	RemainingThresholds []int
	// NotifyEnabled is the plugin-side intent (separate from host toast delivery).
	NotifyEnabled bool
}

// DefaultPluginConfig is the seed default.
var DefaultPluginConfig = PluginConfig{
	RemainingThresholds: append([]int(nil), DefaultRemainingThresholds...),
	NotifyEnabled:       true,
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
	}, "\n")
}

var (
	enabledRe = regexp.MustCompile(`(?i)^enabled\s*=\s*(true|false)\s*$`)
	thrRe     = regexp.MustCompile(`(?i)^remaining_thresholds\s*=\s*\[([^\]]*)\]\s*$`)
)

// ParsePluginConfigTOML is a minimal TOML parser for this plugin's seed format.
func ParsePluginConfigTOML(raw string) PluginConfig {
	notifyEnabled := DefaultPluginConfig.NotifyEnabled
	remaining := append([]int(nil), DefaultPluginConfig.RemainingThresholds...)

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
			continue
		}
		if m := enabledRe.FindStringSubmatch(trimmed); m != nil {
			notifyEnabled = strings.EqualFold(m[1], "true")
			continue
		}
		if m := thrRe.FindStringSubmatch(trimmed); m != nil {
			parts := strings.Split(m[1], ",")
			var nums []int
			ok := true
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				n, err := strconv.Atoi(p)
				if err != nil || n <= 0 || n > 100 {
					ok = false
					break
				}
				nums = append(nums, n)
			}
			if ok && len(nums) > 0 {
				remaining = nums
			}
		}
	}
	return PluginConfig{NotifyEnabled: notifyEnabled, RemainingThresholds: remaining}
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

// LoadPluginConfig loads config.toml or returns defaults.
func LoadPluginConfig(configDir string) PluginConfig {
	path := PluginConfigPath(configDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		return DefaultPluginConfig
	}
	return ParsePluginConfigTOML(string(raw))
}
