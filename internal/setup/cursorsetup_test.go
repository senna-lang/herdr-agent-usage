/**
 * Tests for Cursor setup guidance, including that it never touches Cursor's
 * own configuration.
 */
package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCursorSetupLines_DocumentsEveryConfigLocation(t *testing.T) {
	joined := strings.Join(cursorSetupLines("/plugin"), "\n")
	for _, want := range []string{
		"~/.cursor/cli-config.json",
		"$CURSOR_CONFIG_DIR/cli-config.json",
		"$XDG_CONFIG_HOME/cursor/cli-config.json",
		"/plugin/bin/run-cursor-statusline.sh",
		"chain",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("guidance is missing %q", want)
		}
	}
}

func TestCursorStatusLineSnippet_IsValidConfigFragment(t *testing.T) {
	snippet := CursorStatusLineSnippet("/plugin")
	for _, want := range []string{`"statusLine"`, `"type": "command"`, `/plugin/bin/run-cursor-statusline.sh`} {
		if !strings.Contains(snippet, want) {
			t.Errorf("snippet is missing %q", want)
		}
	}
}

// Setup prints instructions for Cursor and must never edit cli-config.json:
// it is Cursor's file, and a user may already have a statusLine configured
// there that the plugin has no business replacing.
func TestRunSetup_LeavesCursorConfigUntouched(t *testing.T) {
	cursorDir := t.TempDir()
	configPath := filepath.Join(cursorDir, "cli-config.json")
	original := `{"statusLine":{"type":"command","command":"my-own-script.sh"},"version":1}`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	home := t.TempDir()
	report := RunSetup(SetupOptions{Env: map[string]string{
		"HOME":              home,
		"CURSOR_CONFIG_DIR": cursorDir,
		"HERDR_PLUGIN_ROOT": "/plugin",
		"XDG_CONFIG_HOME":   filepath.Join(home, "xdg"),
	}})

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("cursor config disappeared: %v", err)
	}
	if string(after) != original {
		t.Fatalf("cursor config rewritten:\n before %s\n after  %s", original, after)
	}
	afterStat, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !afterStat.ModTime().Equal(before.ModTime()) {
		t.Error("cursor config was rewritten with identical bytes")
	}

	// No plugin-created files may appear beside it either.
	entries, err := os.ReadDir(cursorDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("setup created files in Cursor's config dir: %v", names)
	}

	if !strings.Contains(strings.Join(report.Lines, "\n"), "run-cursor-statusline.sh") {
		t.Error("setup report does not mention the Cursor statusLine entry")
	}
}
