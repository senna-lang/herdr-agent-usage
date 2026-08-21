/**
 * Cursor statusLine setup guidance.
 *
 * Cursor's context usage reaches this plugin only through its statusLine, which
 * the user must configure. Setup deliberately prints instructions and never
 * edits Cursor's configuration: cli-config.json is Cursor's own file, a user may
 * already have a statusLine command there, and replacing it would silently take
 * over a surface the plugin does not own.
 */
package setup

// CursorStatusLineSnippet is the cli-config.json entry enabling Cursor context
// usage, given the plugin root.
func CursorStatusLineSnippet(pluginRoot string) string {
	return `  "statusLine": {
    "type": "command",
    "command": "bash ` + pluginRoot + `/bin/run-cursor-statusline.sh"
  }`
}

// cursorSetupLines renders the Cursor section of the setup report.
//
// The documented configuration locations are listed rather than probed: Cursor
// documents both CURSOR_CONFIG_DIR and, on Linux/BSD, XDG_CONFIG_HOME/cursor,
// but not their precedence, so the guidance names them and leaves the choice to
// the reader instead of asserting an order this plugin cannot verify.
func cursorSetupLines(pluginRoot string) []string {
	return []string{
		"Cursor statusLine (optional, for Cursor context usage):",
		"  Add to Cursor's cli-config.json — this plugin never edits that file.",
		"",
		"  Config location (documented by Cursor):",
		"    ~/.cursor/cli-config.json                     default",
		"    $CURSOR_CONFIG_DIR/cli-config.json            when that variable is set",
		"    $XDG_CONFIG_HOME/cursor/cli-config.json       Linux/BSD, when that variable is set",
		"",
		CursorStatusLineSnippet(pluginRoot),
		"",
		"  If a statusLine command is already configured, keep it and chain:",
		"  have your existing script pass its stdin through to",
		"  " + pluginRoot + "/bin/run-cursor-statusline.sh and print both outputs.",
		"",
	}
}
