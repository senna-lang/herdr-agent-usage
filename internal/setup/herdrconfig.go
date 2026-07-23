/**
 * Inspects and safely appends the toast delivery config in the main Herdr config.toml.
 * Does not overwrite if [ui.toast] already exists.
 */
package setup

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ToastDeliveryStatus is the inspect result for [ui.toast].
type ToastDeliveryStatus struct {
	Kind       string // "missing" | "present"
	Delivery   *string
	RawSnippet string
}

// ResolveHerdrConfigPath returns ~/.config/herdr/config.toml (or overrides).
func ResolveHerdrConfigPath(env map[string]string) string {
	if fromEnv := env["HERDR_CONFIG"]; fromEnv != "" {
		return fromEnv
	}
	if xdg := env["XDG_CONFIG_HOME"]; xdg != "" {
		return filepath.Join(xdg, "herdr", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "herdr", "config.toml")
}

// ToastConfigSnippet is the recommended toast block to append.
func ToastConfigSnippet() string {
	return strings.Join([]string{
		"[ui.toast]",
		`delivery = "herdr" # or "system" / "terminal"`,
		"",
		"[ui.toast.herdr]",
		`position = "bottom-left"`,
		"",
	}, "\n")
}

// SidebarRowsSnippet is the recommended Herdr 0.7.4+ Agent layout. Users with
// an existing [ui.sidebar.agents] section must merge these tokens into it
// instead of appending a duplicate TOML table.
//
// $provider replaces the built-in `agent` token: it renders the harness name
// ("opencode") on a subscription pane and the backend name ("deepseek") on a
// pay-as-you-go one, where the backend is the more informative half. OMP / Pi
// keep the harness name because their backends are in-pane model routing.
func SidebarRowsSnippet() string {
	return strings.Join([]string{
		"[ui.sidebar.agents]",
		"row_gap = 0",
		"rows = [",
		`  ["state_icon", "tab", "pane"],`,
		`  ["$provider", "$limit"],`,
		`  ["$context"],`,
		"]",
		"",
	}, "\n")
}

// KeybindingSnippet is an optional keybinding example block.
// Direct chords (no Herdr prefix mode) so one keypress sequence opens/refreshes.
func KeybindingSnippet() string {
	return strings.Join([]string{
		"[[keys.command]]",
		`key = "ctrl+shift+u"`,
		`type = "plugin_action"`,
		`command = "usagebar.open-limits"`,
		`description = "Agent Usage: open limits pane"`,
		"",
		"[[keys.command]]",
		`key = "ctrl+shift+m"`,
		`type = "plugin_action"`,
		`command = "usagebar.refresh"`,
		`description = "Agent Usage: refresh sidebar meters"`,
		"",
	}, "\n")
}

var (
	deliveryDQ = regexp.MustCompile(`^delivery\s*=\s*"([^"]+)"\s*(#.*)?$`)
	deliverySQ = regexp.MustCompile(`^delivery\s*=\s*'([^']+)'\s*(#.*)?$`)
)

// InspectToastConfig reads [ui.toast] presence and delivery from config body.
func InspectToastConfig(raw string) ToastDeliveryStatus {
	lines := strings.Split(raw, "\n")
	inToast := false
	sawToast := false
	var delivery *string
	var snippetLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if trimmed == "[ui.toast]" || strings.HasPrefix(trimmed, "[ui.toast.") {
				inToast = true
				sawToast = true
				snippetLines = append(snippetLines, line)
				continue
			}
			if inToast && !strings.HasPrefix(trimmed, "[ui.toast") {
				inToast = false
			}
		}
		if inToast {
			snippetLines = append(snippetLines, line)
			if m := deliveryDQ.FindStringSubmatch(trimmed); m != nil {
				d := m[1]
				delivery = &d
			}
			if m := deliverySQ.FindStringSubmatch(trimmed); m != nil {
				d := m[1]
				delivery = &d
			}
		}
	}

	if !sawToast {
		return ToastDeliveryStatus{Kind: "missing"}
	}
	return ToastDeliveryStatus{
		Kind:       "present",
		Delivery:   delivery,
		RawSnippet: strings.TrimRight(strings.Join(snippetLines, "\n"), "\n"),
	}
}

// AppendToastResult is the result of appendToastConfigIfMissing.
type AppendToastResult struct {
	Wrote  bool
	Reason string
}

// AppendToastConfigIfMissing appends toast section only when missing.
func AppendToastConfigIfMissing(configPath string) AppendToastResult {
	dir := filepath.Dir(configPath)
	_ = os.MkdirAll(dir, 0o755)

	raw := ""
	if b, err := os.ReadFile(configPath); err == nil {
		raw = string(b)
		status := InspectToastConfig(raw)
		if status.Kind == "present" {
			if status.Delivery != nil {
				return AppendToastResult{Wrote: false, Reason: "toast already configured (delivery=" + *status.Delivery + ")"}
			}
			return AppendToastResult{Wrote: false, Reason: "toast section already present"}
		}
	}

	base := raw
	if len(raw) > 0 && !strings.HasSuffix(raw, "\n") {
		base = raw + "\n"
	}
	spacer := ""
	if len(base) > 0 && !strings.HasSuffix(base, "\n\n") {
		spacer = "\n"
	}
	block := spacer + "# --- added by Agent Usage (usagebar) setup ---\n" + ToastConfigSnippet()
	_ = os.WriteFile(configPath, []byte(base+block), 0o644)
	return AppendToastResult{Wrote: true, Reason: "appended toast config to " + configPath}
}
