/**
 * Tests for RunSetup
 */
package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSetup_SeedsAndSnippets(t *testing.T) {
	pluginDir := t.TempDir()
	herdrDir := t.TempDir()
	herdrConfig := filepath.Join(herdrDir, "config.toml")
	if err := os.WriteFile(herdrConfig, []byte("[theme]\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := RunSetup(SetupOptions{
		WriteToast: false,
		Env: map[string]string{
			"HERDR_PLUGIN_CONFIG_DIR": pluginDir,
			"HERDR_CONFIG":            herdrConfig,
			"HERDR_PLUGIN_ROOT":       "/tmp/plugin-root",
		},
	})
	if !report.PluginConfigSeeded || report.ToastWrote {
		t.Fatalf("seeded=%v toast=%v", report.PluginConfigSeeded, report.ToastWrote)
	}
	text := strings.Join(report.Lines, "\n")
	for _, want := range []string{
		"seeded plugin config", "toast: NOT configured", "[ui.toast]",
		"[ui.sidebar.agents]", `["state_icon", "$title"]`,
		`"$limit"`, `"$cache_high"`, `"$cache_mid"`, `"$cache_low"`, `"$context"`,
		"usagebar.open-limits", "--write-toast",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

// A Windows plugin root arrives with backslashes. The pasted snippets run it
// through bash and, for Claude, sit inside a JSON string, where backslashes are
// escapes; forward slashes are valid in both. The paste header must also name
// the config file actually resolved, not a hard-coded ~/.config path.
func TestRunSetup_SnippetsUseForwardSlashRootAndResolvedConfigPath(t *testing.T) {
	herdrConfig := filepath.Join(t.TempDir(), "config.toml")
	report := RunSetup(SetupOptions{
		Env: map[string]string{
			"HERDR_PLUGIN_CONFIG_DIR": t.TempDir(),
			"HERDR_CONFIG":            herdrConfig,
			"HERDR_PLUGIN_ROOT":       filepath.FromSlash("/plugins/usagebar"),
		},
	})
	text := strings.Join(report.Lines, "\n")
	for _, want := range []string{
		`"command": "` + statusLineCommand("/plugins/usagebar", "run-statusline.sh", "statusline") + `"`,
		"/statusline " + statusLineCommand("/plugins/usagebar", "run-antigravity-statusline.sh", "antigravity-statusline"),
		"Paste into " + herdrConfig,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestRunSetup_WriteToast(t *testing.T) {
	pluginDir := t.TempDir()
	herdrDir := t.TempDir()
	herdrConfig := filepath.Join(herdrDir, "config.toml")
	if err := os.WriteFile(herdrConfig, []byte("[theme]\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := RunSetup(SetupOptions{
		WriteToast: true,
		Env: map[string]string{
			"HERDR_PLUGIN_CONFIG_DIR": pluginDir,
			"HERDR_CONFIG":            herdrConfig,
		},
	})
	if !report.ToastWrote {
		t.Fatal("expected toast write")
	}
	if !strings.Contains(strings.Join(report.Lines, "\n"), "appended toast config") {
		t.Fatal("missing append message")
	}
}
