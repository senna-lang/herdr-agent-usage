// Command-level tests for the update-check opt-out (issue #134).
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/updatecheck"
)

// updateCheckSpy stands in for updatecheck.Run, recording each invocation and
// returning a fixed result.
type updateCheckSpy struct {
	calls   int
	options []updatecheck.Options
	result  updatecheck.Result
}

func (s *updateCheckSpy) run(options updatecheck.Options) updatecheck.Result {
	s.calls++
	s.options = append(s.options, options)
	return s.result
}

func TestUpdateCheckDisabled(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"unset", map[string]string{}, false},
		{"empty", map[string]string{"USAGEBAR_DISABLE_UPDATE_CHECK": ""}, false},
		{"whitespace", map[string]string{"USAGEBAR_DISABLE_UPDATE_CHECK": " \t "}, false},
		{"one", map[string]string{"USAGEBAR_DISABLE_UPDATE_CHECK": "1"}, true},
		{"any value", map[string]string{"USAGEBAR_DISABLE_UPDATE_CHECK": "false"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := updateCheckDisabled(tc.env); got != tc.want {
				t.Fatalf("updateCheckDisabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCheckForUpdateDisabledNeverChecks ensures USAGEBAR_DISABLE_UPDATE_CHECK
// blocks the checker and state-directory resolution for every flag combination,
// including --force.
func TestCheckForUpdateDisabledNeverChecks(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		quiet bool
	}{
		{"quiet", []string{"--quiet"}, true},
		{"manual", []string{}, false},
		{"force", []string{"--force"}, false},
		{"quiet force", []string{"--quiet", "--force"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			missingDir := filepath.Join(t.TempDir(), "missing")
			env := map[string]string{
				"HERDR_PLUGIN_CONFIG_DIR":       missingDir,
				"USAGEBAR_DISABLE_UPDATE_CHECK": "1",
			}
			spy := &updateCheckSpy{result: updatecheck.Result{Checked: true, Update: true, Latest: "9.9.9", ReleaseURL: "https://example.test"}}
			var stdout, stderr bytes.Buffer

			checkForUpdate(tc.args, env, spy.run, &stdout, &stderr)

			if spy.calls != 0 {
				t.Fatalf("disabled update check invoked the checker %d times", spy.calls)
			}
			if _, err := os.Stat(missingDir); !os.IsNotExist(err) {
				t.Fatalf("disabled update check touched state dir %s (stat err: %v)", missingDir, err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("disabled update check wrote to stderr: %q", stderr.String())
			}
			if tc.quiet {
				if stdout.Len() != 0 {
					t.Fatalf("quiet disabled update check wrote to stdout: %q", stdout.String())
				}
				return
			}
			want := "Agent Usage: update check is disabled by USAGEBAR_DISABLE_UPDATE_CHECK.\n"
			if stdout.String() != want {
				t.Fatalf("stdout = %q, want %q", stdout.String(), want)
			}
		})
	}
}

// TestCheckForUpdateEnabledWhenOptOutBlank ensures unset, empty, and
// whitespace-only values keep the checker running with the expected options.
func TestCheckForUpdateEnabledWhenOptOutBlank(t *testing.T) {
	configDir := t.TempDir()
	values := []struct {
		name string
		env  map[string]string
	}{
		{"unset", map[string]string{"HERDR_PLUGIN_CONFIG_DIR": configDir}},
		{"empty", map[string]string{"HERDR_PLUGIN_CONFIG_DIR": configDir, "USAGEBAR_DISABLE_UPDATE_CHECK": ""}},
		{"whitespace", map[string]string{"HERDR_PLUGIN_CONFIG_DIR": configDir, "USAGEBAR_DISABLE_UPDATE_CHECK": " \t "}},
	}
	for _, tc := range values {
		t.Run(tc.name+" force", func(t *testing.T) {
			spy := &updateCheckSpy{result: updatecheck.Result{Checked: true, Current: "1.0.0"}}
			var stdout, stderr bytes.Buffer

			checkForUpdate([]string{"--current-version", "1.0.0", "--force"}, tc.env, spy.run, &stdout, &stderr)

			if spy.calls != 1 {
				t.Fatalf("checker called %d times, want 1", spy.calls)
			}
			if !spy.options[0].Force {
				t.Fatal("--force must set Options.Force")
			}
			if spy.options[0].StateDir != configDir {
				t.Fatalf("StateDir = %q, want %q", spy.options[0].StateDir, configDir)
			}
			if spy.options[0].CurrentVersion != "1.0.0" {
				t.Fatalf("CurrentVersion = %q, want 1.0.0", spy.options[0].CurrentVersion)
			}
			if stdout.String() != "Agent Usage 1.0.0 is up to date.\n" {
				t.Fatalf("stdout = %q", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
		t.Run(tc.name+" quiet", func(t *testing.T) {
			spy := &updateCheckSpy{result: updatecheck.Result{Checked: true, Update: true, Latest: "2.0.0", Current: "1.0.0", ReleaseURL: "https://example.test"}}
			var stdout, stderr bytes.Buffer

			checkForUpdate([]string{"--quiet", "--force"}, tc.env, spy.run, &stdout, &stderr)

			if spy.calls != 1 {
				t.Fatalf("checker called %d times, want 1", spy.calls)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("quiet check printed stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestCheckForUpdateReportsAvailableUpdate(t *testing.T) {
	spy := &updateCheckSpy{result: updatecheck.Result{
		Checked:    true,
		Update:     true,
		Current:    "0.1.0",
		Latest:     "0.2.0",
		ReleaseURL: "https://example.test/release",
	}}
	var stdout, stderr bytes.Buffer

	checkForUpdate(nil, map[string]string{"HERDR_PLUGIN_CONFIG_DIR": t.TempDir()}, spy.run, &stdout, &stderr)

	want := "Agent Usage update available: 0.2.0 (installed 0.1.0)\n" +
		"Release and update instructions: https://example.test/release\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCheckForUpdateReportsErrorToStderr(t *testing.T) {
	spy := &updateCheckSpy{result: updatecheck.Result{Err: errors.New("boom")}}
	var stdout, stderr bytes.Buffer

	checkForUpdate(nil, map[string]string{"HERDR_PLUGIN_CONFIG_DIR": t.TempDir()}, spy.run, &stdout, &stderr)

	if stderr.String() != "Agent Usage: could not check for updates: boom\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
