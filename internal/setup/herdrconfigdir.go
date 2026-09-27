package setup

import (
	"os"
	"path/filepath"
	"runtime"
)

// herdrConfigDir returns the directory holding Herdr's own config.toml and its
// plugins/config tree, matching where the Herdr binary itself looks:
// $XDG_CONFIG_HOME/herdr when set, %APPDATA%\herdr on Windows, and
// ~/.config/herdr otherwise. The statusLine bridge runs inside the agent
// process rather than under a Herdr hook, so it cannot rely on Herdr exporting
// these paths and must derive the same location on its own.
func herdrConfigDir(env map[string]string) string {
	return herdrConfigDirFor(env, runtime.GOOS)
}

func herdrConfigDirFor(env map[string]string, goos string) string {
	if xdg := env["XDG_CONFIG_HOME"]; xdg != "" {
		return filepath.Join(xdg, "herdr")
	}
	if goos == "windows" {
		appData := env["APPDATA"]
		if appData == "" {
			appData, _ = os.UserConfigDir()
		}
		if appData != "" {
			return filepath.Join(appData, "herdr")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "herdr")
}
