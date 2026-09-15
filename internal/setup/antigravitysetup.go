/**
 * Antigravity CLI statusLine setup guidance.
 *
 * Antigravity's context and quota usage reach this plugin only through its
 * statusLine, which the user must configure via the `/statusline <command>`
 * slash command inside `agy` itself — there is no config file to edit, unlike
 * Cursor's cli-config.json. Configuring it fully replaces Antigravity's
 * built-in status line rather than chaining with it.
 */
package setup

// AntigravityStatusLineSnippet is the `/statusline` command enabling
// Antigravity context and quota usage, given the plugin root.
func AntigravityStatusLineSnippet(pluginRoot string) string {
	return `/statusline bash ` + pluginRoot + `/bin/run-antigravity-statusline.sh`
}

// antigravitySetupLines renders the Antigravity section of the setup report.
func antigravitySetupLines(pluginRoot string) []string {
	return []string{
		"Antigravity CLI statusLine (optional, for Antigravity context + quota usage):",
		"  Run inside `agy` itself — there is no config file to edit:",
		"",
		"  " + AntigravityStatusLineSnippet(pluginRoot),
		"",
		"  This replaces Antigravity's built-in status line with the one this",
		"  plugin renders. Run `/statusline delete` inside agy to revert.",
		"",
	}
}
