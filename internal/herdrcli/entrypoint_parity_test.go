/**
 * Pins the herdr executable-resolution contract shared by the Go callbacks
 * (herdrBin) and the plugin's limits-pane shell entrypoint: both must use
 * HERDR_BIN_PATH only when it names a runnable regular file, and otherwise fall
 * back to herdr on PATH. A stale HERDR_BIN_PATH made every callback fail with
 * ENOENT (#94), and the entrypoint resolved the same value on its own, so the
 * policy is asserted against both.
 */
package herdrcli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// openLimitsPaneScript is the plugin entrypoint that resolves HERDR_BIN_PATH
// outside this package. This package owns the resolution policy, so it owns the
// parity test.
var openLimitsPaneScript = filepath.Join("..", "..", "bin", "open-limits-pane.sh")

// openLimitsPaneArgs is what the entrypoint passes to the herdr action declared
// in herdr-plugin.toml.
const openLimitsPaneArgs = "plugin pane open --plugin usagebar --entrypoint limits --placement split --direction right --no-focus"

// entrypointFixtures holds the HERDR_BIN_PATH shapes under test plus the herdr
// stand-ins that record which executable was invoked.
type entrypointFixtures struct {
	pathDir     string // PATH entry holding the fallback herdr
	override    string // executable file
	noExec      string // regular file without the executable bit
	dirValue    string // directory
	symlink     string // symlink to override
	dangling    string // symlink to a missing target
	stale       string // path under a removed version directory
	pathLog     string
	overrideLog string
}

func newEntrypointFixtures(t *testing.T) entrypointFixtures {
	t.Helper()
	root := t.TempDir()
	f := entrypointFixtures{
		pathDir:     filepath.Join(root, "path-bin"),
		override:    filepath.Join(root, "override-herdr"),
		noExec:      filepath.Join(root, "not-executable"),
		dirValue:    filepath.Join(root, "a-directory"),
		symlink:     filepath.Join(root, "herdr-symlink"),
		dangling:    filepath.Join(root, "dangling-herdr"),
		stale:       filepath.Join(root, "0.8.2", "herdr"),
		pathLog:     filepath.Join(root, "path-shim.log"),
		overrideLog: filepath.Join(root, "override-shim.log"),
	}
	if err := os.Mkdir(f.pathDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeHerdrShim(t, filepath.Join(f.pathDir, "herdr"), f.pathLog)
	writeHerdrShim(t, f.override, f.overrideLog)
	if err := os.WriteFile(f.noExec, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.dirValue, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.override, f.symlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "removed"), f.dangling); err != nil {
		t.Fatal(err)
	}
	return f
}

// writeHerdrShim writes an executable herdr stand-in that records the arguments
// it was invoked with.
func writeHerdrShim(t *testing.T, path, logPath string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + logPath + "'\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestHerdrBinPathResolution_ParityWithLimitsPaneEntrypoint(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash is required to exercise the plugin entrypoint: %v", err)
	}

	for _, tt := range []struct {
		name  string
		value func(f entrypointFixtures) string
		// resolves is the path the value must resolve to, or nil when both
		// implementations must fall back to herdr on PATH.
		resolves func(f entrypointFixtures) string
	}{
		{name: "unset", value: func(entrypointFixtures) string { return "" }},
		{name: "removed version directory", value: func(f entrypointFixtures) string { return f.stale }},
		{name: "regular file without the executable bit", value: func(f entrypointFixtures) string { return f.noExec }},
		{name: "executable file", value: func(f entrypointFixtures) string { return f.override }, resolves: func(f entrypointFixtures) string { return f.override }},
		{name: "directory", value: func(f entrypointFixtures) string { return f.dirValue }},
		{name: "symlink to a missing target", value: func(f entrypointFixtures) string { return f.dangling }},
		{name: "symlink to an executable", value: func(f entrypointFixtures) string { return f.symlink }, resolves: func(f entrypointFixtures) string { return f.symlink }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newEntrypointFixtures(t)
			value := tt.value(f)
			wantBin, wantLog, otherLog := "herdr", f.pathLog, f.overrideLog
			if tt.resolves != nil {
				wantBin, wantLog, otherLog = tt.resolves(f), f.overrideLog, f.pathLog
			}

			t.Setenv("PATH", f.pathDir)
			t.Setenv("HERDR_BIN_PATH", value)
			var gotBin string
			captureStderr(t, func() { gotBin = herdrBin() })
			if gotBin != wantBin {
				t.Fatalf("herdrBin() = %q, want %q", gotBin, wantBin)
			}

			// PATH holds only the fallback shim, so the entrypoint can reach
			// the override only through HERDR_BIN_PATH itself.
			cmd := exec.Command(bash, openLimitsPaneScript)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed: %v; output = %s", openLimitsPaneScript, err, out)
			}
			assertHerdrInvocation(t, wantLog, otherLog)
		})
	}
}

// assertHerdrInvocation requires exactly one herdr stand-in to have run, with
// the limits-pane arguments.
func assertHerdrInvocation(t *testing.T, wantLog, otherLog string) {
	t.Helper()
	data, err := os.ReadFile(wantLog)
	if err != nil {
		t.Fatalf("herdr %s was not invoked: %v", wantLog, err)
	}
	if got := strings.TrimSpace(string(data)); got != openLimitsPaneArgs {
		t.Fatalf("herdr arguments = %q, want %q", got, openLimitsPaneArgs)
	}
	if _, err := os.Stat(otherLog); err == nil {
		t.Fatalf("unexpected herdr %s invoked", otherLog)
	}
}
