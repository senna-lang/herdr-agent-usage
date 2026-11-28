// Review141 regressions for collect --format and collect/notify --panes.
// Malformed values must be rejected before any collection or the notify
// enabled shortcut.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReview141CLIChild is the re-exec target for the real usagebar main.
// It is not a scenario by itself.
func TestReview141CLIChild(t *testing.T) {
	raw := os.Getenv("USAGEBAR_REVIEW141_ARGS")
	if raw == "" {
		t.Skip("only runs as a child of the Review141 CLI tests")
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatal(err)
	}
	os.Args = append([]string{"usagebar"}, args...)
	main()
	os.Exit(0)
}

func TestReview141MalformedFlags(t *testing.T) {
	nullStdin := "null\n"
	emptyStdin := ""
	for _, tc := range []struct {
		name    string
		enabled bool
		args    []string
		stdin   *string
		flag    string
	}{
		{name: "collect format missing", args: []string{"collect", "--format"}, flag: "--format"},
		{name: "collect format empty", args: []string{"collect", "--format", ""}, flag: "--format"},
		{name: "collect format next option", args: []string{"collect", "--format", "--quotas-only"}, flag: "--format"},
		{name: "collect panes missing", args: []string{"collect", "--panes"}, flag: "--panes"},
		{name: "collect panes empty", args: []string{"collect", "--panes", ""}, flag: "--panes"},
		{name: "collect panes next option", args: []string{"collect", "--panes", "--all"}, flag: "--panes"},
		{name: "collect panes null", args: []string{"collect", "--panes", "null"}, flag: "--panes"},
		{name: "collect panes stdin null", args: []string{"collect", "--panes", "-"}, stdin: &nullStdin, flag: "--panes"},
		{name: "collect panes stdin empty", args: []string{"collect", "--panes", "-"}, stdin: &emptyStdin, flag: "--panes"},
		{name: "notify panes missing", args: []string{"notify", "--json", "--panes"}, flag: "--panes"},
		{name: "notify panes empty", args: []string{"notify", "--json", "--panes", ""}, flag: "--panes"},
		{name: "notify panes next option", args: []string{"notify", "--json", "--panes", "--format"}, flag: "--panes"},
		{name: "notify panes null", args: []string{"notify", "--json", "--panes", "null"}, flag: "--panes"},
		{name: "notify panes stdin null", args: []string{"notify", "--json", "--panes", "-"}, stdin: &nullStdin, flag: "--panes"},
		{name: "notify panes stdin empty", args: []string{"notify", "--json", "--panes", "-"}, stdin: &emptyStdin, flag: "--panes"},
		{name: "notify panes null while enabled", enabled: true, args: []string{"notify", "--json", "--panes", "null"}, flag: "--panes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := review141Sandbox(t, tc.enabled)
			before := review141Snapshot(t, root)

			got := review141Run(t, root, tc.stdin, tc.args...)

			if got.code != 2 || got.stdout != "" || !strings.Contains(got.stderr, tc.flag) {
				t.Fatalf("exit %d, stdout %q, stderr %q; want exit 2, empty stdout, stderr naming %s", got.code, got.stdout, got.stderr, tc.flag)
			}
			if after := review141Snapshot(t, root); after != before {
				t.Fatalf("malformed flags changed the sandbox\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestReview141EmptyPanesAccepted(t *testing.T) {
	arrayStdin := "[]\n"
	for _, tc := range []struct {
		name  string
		args  []string
		stdin *string
	}{
		{name: "collect literal", args: []string{"collect", "--quotas-only", "--panes", "[]"}},
		{name: "collect format v1", args: []string{"collect", "--quotas-only", "--format", "v1", "--panes", "[]"}},
		{name: "collect stdin", args: []string{"collect", "--quotas-only", "--panes", "-"}, stdin: &arrayStdin},
		{name: "notify literal", args: []string{"notify", "--json", "--panes", "[]"}},
		{name: "notify stdin", args: []string{"notify", "--json", "--panes", "-"}, stdin: &arrayStdin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := review141Sandbox(t, false)

			got := review141Run(t, root, tc.stdin, tc.args...)

			if got.code != 0 || !json.Valid([]byte(got.stdout)) {
				t.Fatalf("exit %d, stdout %q, stderr %q; want exit 0 and JSON stdout", got.code, got.stdout, got.stderr)
			}
			if _, err := os.Stat(filepath.Join(root, "herdr-called")); err == nil {
				t.Fatal("accepted [] queried herdr")
			}
		})
	}
}

func TestReview141StatusLineWritesAccountProvenance(t *testing.T) {
	root := review141Sandbox(t, false)
	home := filepath.Join(root, "home")
	jsonPath := filepath.Join(home, ".claude.json")
	cachePath := filepath.Join(root, "claude-limits.json")
	const (
		email = "a@x.com"
		org   = "44444444-4444-4444-8444-444444444444"
	)
	raw, err := json.Marshal(map[string]any{
		"oauthAccount": map[string]any{
			"emailAddress":     email,
			"organizationUuid": org,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	stdin := `{"rate_limits":{"five_hour":{"used_percentage":55,"resets_at":1800000000}}}`
	env := review141Env(root)
	env = append(env, "CLAUDE_CONFIG_JSON="+jsonPath, "USAGEBAR_CLAUDE_LIMITS_PATH="+cachePath)

	got := review141RunEnv(t, env, &stdin, "statusline")

	if got.code != 0 {
		t.Fatalf("exit %d, stderr %q; want statusline to write the cache", got.code, got.stderr)
	}
	written, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache was not written: %v\nstderr %q", err, got.stderr)
	}
	var parsed map[string]any
	if err := json.Unmarshal(written, &parsed); err != nil {
		t.Fatalf("decode cache: %v\n%s", err, written)
	}
	if parsed["accountEmail"] != email || parsed["organizationUuid"] != org {
		t.Fatalf("cache provenance = %v/%v, want %s/%s\n%s", parsed["accountEmail"], parsed["organizationUuid"], email, org, written)
	}
	five, _ := parsed["fiveHour"].(map[string]any)
	if five == nil || five["usedPercentage"] != 55.0 {
		t.Fatalf("cache window = %v, want fiveHour usedPercentage 55\n%s", parsed["fiveHour"], written)
	}
}

func review141Sandbox(t *testing.T, notifyEnabled bool) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"home", "plugin", "history", "state", "tmp"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	enabled := "false"
	if notifyEnabled {
		enabled = "true"
	}
	config := "[notify]\nenabled = " + enabled + "\n"
	if err := os.WriteFile(filepath.Join(root, "plugin", "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	herdr := "#!/bin/sh\nprintf 'called\\n' >> \"$USAGEBAR_TEST_HERDR_LOG\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(root, "herdr"), []byte(herdr), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

type review141Result struct {
	code   int
	stdout string
	stderr string
}

func review141Run(t *testing.T, root string, stdin *string, args ...string) review141Result {
	t.Helper()
	return review141RunEnv(t, review141Env(root), stdin, args...)
}

func review141RunEnv(t *testing.T, env []string, stdin *string, args ...string) review141Result {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	testArgs := []string{"-test.run=^TestReview141CLIChild$"}
	env = append([]string{}, env...)
	if cover := flag.Lookup("test.gocoverdir"); cover != nil && cover.Value.String() != "" {
		testArgs = append(testArgs, "-test.gocoverdir="+cover.Value.String())
		env = append(env, "GOCOVERDIR="+cover.Value.String())
	}
	env = append(env, "USAGEBAR_REVIEW141_ARGS="+string(encoded))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], testArgs...)
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = strings.NewReader(*stdin)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("usagebar %s timed out; stdout %q stderr %q", strings.Join(args, " "), stdout.String(), stderr.String())
	}
	if err == nil {
		return review141Result{code: 0, stdout: stdout.String(), stderr: stderr.String()}
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatal(err)
	}
	return review141Result{code: exitErr.ExitCode(), stdout: stdout.String(), stderr: stderr.String()}
}

func review141Env(root string) []string {
	home := filepath.Join(root, "home")
	return []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + home,
		"TMPDIR=" + filepath.Join(root, "tmp"),
		"HERDR_PLUGIN_CONFIG_DIR=" + filepath.Join(root, "plugin"),
		"CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude"),
		"CODEX_HOME=" + filepath.Join(home, ".codex"),
		"GROK_HOME=" + filepath.Join(home, ".grok"),
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"USAGEBAR_HISTORY_PATH=" + filepath.Join(root, "history", "history.json"),
		"USAGEBAR_STATE_DIR=" + filepath.Join(root, "state"),
		"USAGEBAR_PROVIDER_NOTIFY_PATH=" + filepath.Join(root, "provider-state.json"),
		"USAGEBAR_DISABLE_BROWSER_COOKIES=1",
		"USAGEBAR_OMP_AGENT_DB=" + filepath.Join(root, "absent-agent.db"),
		"OPENCODE_DB=" + filepath.Join(root, "absent-opencode.db"),
		"OPENCODE_DATA_DIR=" + filepath.Join(root, "opencode-data"),
		"KILO_DB=" + filepath.Join(root, "absent-kilo.db"),
		"KILO_DATA_DIR=" + filepath.Join(root, "kilo-data"),
		"DEVIN_DATA_DIR=" + filepath.Join(root, "devin-data"),
		"HERMES_HOME=" + filepath.Join(root, "hermes"),
		"PI_SESSIONS_ROOT=" + filepath.Join(root, "pi-sessions"),
		"OMP_SESSIONS_ROOT=" + filepath.Join(root, "omp-sessions"),
		"OMP_MODELS_DB=" + filepath.Join(root, "absent-models.db"),
		"PI_MODELS_DB=" + filepath.Join(root, "absent-pi-models.db"),
		"HERDR_BIN_PATH=" + filepath.Join(root, "herdr"),
		"USAGEBAR_TEST_HERDR_LOG=" + filepath.Join(root, "herdr-called"),
		"USAGEBAR_DISABLE_UPDATE_CHECK=1",
	}
}

func review141Snapshot(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "tmp" || strings.HasPrefix(rel, "tmp"+string(os.PathSeparator)) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.WriteString(rel)
		b.WriteString("\n")
		b.Write(data)
		b.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
