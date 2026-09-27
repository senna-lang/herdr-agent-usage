package setup

import "runtime"

// statusLineCommand is the command an agent's statusLine setting runs to feed
// its payload to usagebar. subcommand is the usagebar subcommand, and
// wrapperScript the bin/ script that runs it on Unix.
//
// On Unix the command goes through the bash wrapper, which resolves
// $USAGEBAR_BIN and PATH fallbacks. On Windows it names the binary directly:
// the agent may start the command through cmd.exe or PowerShell, where a bare
// `bash` resolves to WSL (System32 is searched before PATH) and would run a
// Linux binary against the WSL home directory.
func statusLineCommand(pluginRoot, wrapperScript, subcommand string) string {
	return statusLineCommandFor(pluginRoot, wrapperScript, subcommand, runtime.GOOS)
}

func statusLineCommandFor(pluginRoot, wrapperScript, subcommand, goos string) string {
	if goos == "windows" {
		return pluginRoot + "/bin/usagebar.exe " + subcommand
	}
	return "bash " + pluginRoot + "/bin/" + wrapperScript
}
