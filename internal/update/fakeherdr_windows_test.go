//go:build windows

package update

import (
	"os"
	"os/exec"
	"testing"
)

// writeHerdrShim installs script as a herdr stand-in and returns the path to
// put in HERDR_BIN_PATH, or whose directory to put on PATH. Windows cannot run
// a #!/bin/sh file directly, so the script sits beside a path+".cmd" wrapper
// that runs it through bash, the same interpreter the plugin's Herdr hooks
// require. bash is resolved to an absolute path now because tests narrow PATH.
func writeHerdrShim(t *testing.T, path, script string) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash is required for the herdr shim on Windows: %v", err)
	}
	scriptPath := path + ".sh"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	wrapper := path + ".cmd"
	body := "@\"" + bash + "\" \"" + scriptPath + "\" %*\r\n"
	if err := os.WriteFile(wrapper, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return wrapper
}
