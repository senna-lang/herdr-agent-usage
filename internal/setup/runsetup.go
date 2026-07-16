/**
 * Orchestrates the usagebar setup.
 * - Seed the plugin config
 * - Inspect the Herdr toast config
 * - Optionally append the toast config (--write-toast)
 * - Print snippets to paste
 */
package setup

import (
	"os"
	"strings"
)

// SetupOptions configures runSetup.
type SetupOptions struct {
	// WriteToast appends toast config when missing.
	WriteToast bool
	Env        map[string]string
}

// SetupReport is the setup command output.
type SetupReport struct {
	Lines              []string
	PluginConfigSeeded bool
	ToastWrote         bool
}

// RunSetup seeds plugin config, optionally writes toast, and prints snippets.
func RunSetup(options SetupOptions) SetupReport {
	env := options.Env
	if env == nil {
		env = envFromOS()
	}
	var lines []string
	pluginDir := ResolvePluginConfigDir(env)
	herdrConfigPath := ResolveHerdrConfigPath(env)

	lines = append(lines, "Agent Usage · setup", "──────────────────")

	seeded := SeedPluginConfigIfMissing(pluginDir)
	pluginPath := PluginConfigPath(pluginDir)
	pluginCfg := LoadPluginConfig(pluginDir)
	if seeded {
		lines = append(lines, "✓ seeded plugin config: "+pluginPath)
	} else {
		lines = append(lines, "· plugin config exists: "+pluginPath)
	}
	thr := make([]string, len(pluginCfg.RemainingThresholds))
	for i, n := range pluginCfg.RemainingThresholds {
		thr[i] = itoa(n)
	}
	lines = append(lines,
		"  notify.enabled="+boolStr(pluginCfg.NotifyEnabled)+"  thresholds=["+strings.Join(thr, ", ")+"]",
		"",
	)

	toastWrote := false
	if options.WriteToast {
		result := AppendToastConfigIfMissing(herdrConfigPath)
		toastWrote = result.Wrote
		if result.Wrote {
			lines = append(lines, "✓ "+result.Reason, "  run: herdr server reload-config", "")
		} else {
			lines = append(lines, "· "+result.Reason, "")
		}
	}

	toastStatusText := "Herdr config not found yet"
	if _, err := os.Stat(herdrConfigPath); err == nil {
		raw, _ := os.ReadFile(herdrConfigPath)
		status := InspectToastConfig(string(raw))
		switch {
		case status.Kind == "missing":
			toastStatusText = "toast: NOT configured (notifications may not appear)"
		case status.Delivery == nil:
			toastStatusText = "toast: section present, delivery not set"
		default:
			toastStatusText = "toast: delivery=" + *status.Delivery
		}
	}
	lines = append(lines,
		"Herdr config: "+herdrConfigPath,
		"  "+toastStatusText,
		"",
		"── Paste into ~/.config/herdr/config.toml ──",
		"",
		"# Toast delivery (required for rate-limit notifications)",
		strings.TrimRight(ToastConfigSnippet(), "\n"),
		"",
		"# Optional keybindings",
		strings.TrimRight(KeybindingSnippet(), "\n"),
		"",
		"Then: herdr server reload-config",
		"",
	)
	if !options.WriteToast {
		lines = append(lines,
			"Tip: run with --write-toast to append the toast block automatically",
			"     (only if [ui.toast] is missing; never overwrites).",
			"",
		)
	}
	root := env["HERDR_PLUGIN_ROOT"]
	if root == "" {
		root = "/path/to/herdr-agent-usage"
	}
	lines = append(lines,
		"Claude statusLine (optional, for CC rate windows):",
		`  "command": "bash `+root+`/bin/run-statusline.sh"`,
		"",
	)

	return SetupReport{Lines: lines, PluginConfigSeeded: seeded, ToastWrote: toastWrote}
}

func envFromOS() map[string]string {
	env := map[string]string{}
	for _, e := range os.Environ() {
		if i := strings.IndexByte(e, '='); i >= 0 {
			env[e[:i]] = e[i+1:]
		}
	}
	return env
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
