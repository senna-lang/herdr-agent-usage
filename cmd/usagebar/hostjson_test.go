// Command-level tests for the host-facing JSON entrypoints
// (`usagebar notify`, `notify --json` and `collect`).
package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

// isolateHostEnv points every config, state and history path at temp dirs,
// installs a fake herdr that logs each call, and seeds a Claude limits cache
// reading usedPercent. It returns the path the fake herdr logs to; the file
// exists only after herdr was queried.
func isolateHostEnv(t *testing.T, configTOML string, usedPercent int) (herdrCalled string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("GROK_HOME", filepath.Join(home, ".grok"))
	t.Setenv("HERDR_PANE_ID", "")
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", "")
	t.Setenv("USAGEBAR_HISTORY_PATH", filepath.Join(home, "history.json"))
	t.Setenv("USAGEBAR_STATE_DIR", filepath.Join(home, "state"))
	t.Setenv("USAGEBAR_PROVIDER_NOTIFY_PATH", filepath.Join(home, "provider-state.json"))
	for _, key := range []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "CLAUDE_CODE_USE_MANTLE"} {
		t.Setenv(key, "")
	}

	pluginDir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginDir)
	if err := os.WriteFile(filepath.Join(pluginDir, "config.toml"), []byte(configTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	herdrCalled = filepath.Join(home, "herdr-called")
	t.Setenv("USAGEBAR_TEST_HERDR_LOG", herdrCalled)
	herdr := filepath.Join(home, "herdr")
	script := "#!/bin/sh\nprintf 'called\\n' >> \"$USAGEBAR_TEST_HERDR_LOG\"\nexit 1\n"
	if err := os.WriteFile(herdr, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", herdr)

	cache := limits.ResolvedClaudeProfiles()[0].LimitsCache
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	nowMs := time.Now().UnixMilli()
	raw, err := json.Marshal(map[string]any{
		"fetchedAtMs": nowMs,
		"fiveHour":    map[string]any{"usedPercentage": usedPercent, "resetsAt": nowMs/1000 + 3_600},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return herdrCalled
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// captureStdout returns what fn writes to os.Stdout.
func captureStdout(t *testing.T, fn func()) []byte {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer func() { os.Stdout = original }()
	os.Stdout = writer
	fn()
	writer.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestChildEntry is not a test: runChild re-executes the test binary into it
// so entrypoints that call os.Exit can be observed from outside.
func TestChildEntry(t *testing.T) {
	name := os.Getenv("USAGEBAR_TEST_CHILD")
	if name == "" {
		t.Skip("only runs as a child of runChild")
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("USAGEBAR_TEST_CHILD_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	switch name {
	case "notify-json":
		runNotifyJSON(args)
	case "collect-json":
		runCollectJSON(args)
	}
	os.Exit(0)
}

// runChild runs one entrypoint in a subprocess and returns its exit code and
// stderr.
func runChild(t *testing.T, entry string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestChildEntry$")
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "USAGEBAR_TEST_CHILD="+entry, "USAGEBAR_TEST_CHILD_ARGS="+string(encoded))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		return 0, stderr.String()
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatal(err)
	}
	return exitErr.ExitCode(), stderr.String()
}

const claudePaneJSON = `[{"paneId":"p1","agent":"claude"}]`

func decodePendingToasts(t *testing.T, raw []byte) limits.PendingToastsReport {
	t.Helper()
	var report limits.PendingToastsReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return report
}

// herdr delivery is the only mode in which `usagebar notify` itself may talk
// to herdr; with [notify] delivery = "external" it must stay silent and never
// query it.
func TestRunNotifyHonoursDeliveryMode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		config    string
		wantHerdr bool
	}{
		{"herdr delivery", "[notify]\nenabled = true\n", true},
		{"external delivery", "[notify]\nenabled = true\ndelivery = \"external\"\n", false},
		{"disabled", "[notify]\nenabled = false\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			herdrCalled := isolateHostEnv(t, tc.config, 85)
			runNotify()
			if got := fileExists(herdrCalled); got != tc.wantHerdr {
				t.Fatalf("herdr queried=%v, want %v", got, tc.wantHerdr)
			}
		})
	}
}

func TestRunNotifyJSONDisabledPrintsAnEmptyList(t *testing.T) {
	herdrCalled := isolateHostEnv(t, "[notify]\nenabled = false\n", 85)

	raw := captureStdout(t, func() { runNotifyJSON([]string{"--panes", claudePaneJSON}) })

	report := decodePendingToasts(t, raw)
	if report.Version != limits.CollectReportVersion || report.Toasts == nil || len(report.Toasts) != 0 {
		t.Fatalf("report = %s, want an empty toast list", raw)
	}
	if !strings.Contains(string(raw), `"toasts": []`) {
		t.Fatalf("toasts must encode as [] not null: %s", raw)
	}
	if fileExists(herdrCalled) {
		t.Fatal("disabled notifications queried herdr")
	}
}

func TestRunNotifyJSONReturnsDueToastsFromSuppliedPanes(t *testing.T) {
	isolateHostEnv(t, "[notify]\nenabled = true\ndelivery = \"external\"\n", 85)

	raw := captureStdout(t, func() { runNotifyJSON([]string{"--panes", claudePaneJSON}) })

	report := decodePendingToasts(t, raw)
	if len(report.Toasts) != 1 || report.Toasts[0].ProviderID != "claude" || report.Toasts[0].Bucket != 20 {
		t.Fatalf("report = %s, want one claude toast at the 20%% bucket", raw)
	}
}

// Pay-as-you-go setups never get subscription-window toasts.
func TestRunNotifyJSONBillingGate(t *testing.T) {
	isolateHostEnv(t, "[notify]\nenabled = true\ndelivery = \"external\"\n", 85)
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")

	raw := captureStdout(t, func() { runNotifyJSON([]string{"--panes", claudePaneJSON}) })

	if report := decodePendingToasts(t, raw); len(report.Toasts) != 0 {
		t.Fatalf("pay-as-you-go setup got toasts: %s", raw)
	}
}

func TestRunNotifyJSONRejectsBadPanes(t *testing.T) {
	isolateHostEnv(t, "[notify]\nenabled = true\n", 85)

	code, stderr := runChild(t, "notify-json", "--panes", "not json")

	if code != 2 || !strings.Contains(stderr, "usagebar notify: --panes") {
		t.Fatalf("exit %d, stderr %q; want exit 2 naming --panes", code, stderr)
	}
}

func TestRunCollectJSONRejectsUnknownFormat(t *testing.T) {
	isolateHostEnv(t, "", 32)

	code, stderr := runChild(t, "collect-json", "--format", "v2")

	if code != 2 || !strings.Contains(stderr, `unknown --format "v2"`) {
		t.Fatalf("exit %d, stderr %q; want exit 2 naming the format", code, stderr)
	}
}

// --format v1 encodes the versioned report, not the legacy Go-shaped one.
func TestRunCollectJSONV1EndToEnd(t *testing.T) {
	isolateHostEnv(t, "", 32)

	raw := captureStdout(t, func() { runCollectJSON([]string{"--all", "--quotas-only", "--format", "v1"}) })

	var report limits.CollectReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	if report.Version != limits.CollectReportVersion || report.Filter != limits.CollectFilterAll || report.GeneratedAtMs == 0 {
		t.Fatalf("report header = %+v", report)
	}
	var claudeWindows []limits.WindowReport
	for _, p := range report.Providers {
		if p.ProviderID == "claude" {
			claudeWindows = p.Windows
		}
	}
	if len(claudeWindows) != 1 || claudeWindows[0].Slot != "primary" || claudeWindows[0].UsedPercent != 32 || claudeWindows[0].RemainingPercent != 68 {
		t.Fatalf("claude windows = %+v in %s", claudeWindows, raw)
	}
	if !strings.Contains(string(raw), `"providerId"`) || strings.Contains(string(raw), `"ProviderID"`) {
		t.Fatalf("output is not the v1 shape: %s", raw)
	}
}

func TestCollectPaneSource(t *testing.T) {
	t.Run("inline json", func(t *testing.T) {
		list, err := collectPaneSource([]string{"--panes", claudePaneJSON})
		if err != nil {
			t.Fatal(err)
		}
		snaps, ok := list()
		if !ok || len(snaps) != 1 || snaps[0].Agent != "claude" {
			t.Fatalf("snaps = %+v ok=%v", snaps, ok)
		}
	})

	t.Run("stdin", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.WriteString(`[{"paneId":"p9","agent":"codex"}]`); err != nil {
			t.Fatal(err)
		}
		writer.Close()
		original := os.Stdin
		os.Stdin = reader
		defer func() { os.Stdin = original; reader.Close() }()

		list, err := collectPaneSource([]string{"--panes", "-"})
		if err != nil {
			t.Fatal(err)
		}
		snaps, ok := list()
		if !ok || len(snaps) != 1 || snaps[0].PaneID != "p9" || snaps[0].Agent != "codex" {
			t.Fatalf("snaps = %+v ok=%v", snaps, ok)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		if _, err := collectPaneSource([]string{"--panes", "{nope"}); err == nil || !strings.Contains(err.Error(), "--panes") {
			t.Fatalf("err = %v, want a --panes parse error", err)
		}
	})

	t.Run("absent flag falls back to herdr", func(t *testing.T) {
		list, err := collectPaneSource(nil)
		if err != nil || list == nil {
			t.Fatalf("list=%v err=%v", list, err)
		}
	})
}
