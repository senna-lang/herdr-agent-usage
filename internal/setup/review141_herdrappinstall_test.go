// Review141 regression for scripts/herdr-app-install.sh. A first install whose
// cp or mv fails after one artifact is in place must remove every path that
// attempt created, including temps and the transient manifest, while leaving
// config and unrelated files alone. Retry and uninstall then succeed.
package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReview141InstallPartialFailure(t *testing.T) {
	t.Run("cp fails after one copied artifact", func(t *testing.T) {
		review141FirstInstallFailure(t, 1, 0)
	})
	t.Run("mv fails after one installed artifact", func(t *testing.T) {
		review141FirstInstallFailure(t, 0, 1)
	})
	t.Run("failed upgrade preserves ownership", func(t *testing.T) {
		review141UpgradeFailure(t)
	})
}

func review141FirstInstallFailure(t *testing.T, failCP, failMV int) {
	t.Helper()
	box := newReview141InstallBox(t)

	first := box.run("install", failCP, failMV)
	if first.code == 0 {
		t.Fatalf("partial install exited 0\n%s", first.out)
	}
	box.keepsEqual()
	if leftovers := box.ownedLeftovers(); len(leftovers) != 0 {
		t.Errorf("failed first install left paths: %s\n%s", strings.Join(leftovers, ", "), first.out)
	}

	retry := box.run("install", 0, 0)
	if retry.code != 0 {
		t.Fatalf("retry exit %d, want 0\n%s", retry.code, retry.out)
	}
	box.assertInstalled()
	box.keepsEqual()

	removed := box.run("uninstall", 0, 0)
	if removed.code != 0 {
		t.Fatalf("uninstall exit %d, want 0\n%s", removed.code, removed.out)
	}
	box.assertUninstalled()
	box.keepsEqual()
}

func review141UpgradeFailure(t *testing.T) {
	t.Helper()
	box := newReview141InstallBox(t)

	installed := box.run("install", 0, 0)
	if installed.code != 0 {
		t.Fatalf("seed install exit %d, want 0\n%s", installed.code, installed.out)
	}
	box.assertInstalled()
	manifest, err := os.ReadFile(box.manifest)
	if err != nil {
		t.Fatal(err)
	}

	failed := box.run("install", 1, 0)
	if failed.code == 0 {
		t.Fatalf("upgrade failure exited 0\n%s", failed.out)
	}
	box.keepsEqual()
	if got, err := os.ReadFile(box.manifest); err != nil || !bytes.Equal(got, manifest) {
		t.Fatalf("failed upgrade changed ownership manifest: %v\n%s", err, got)
	}
	if _, err := os.Stat(box.manifestNew); !os.IsNotExist(err) {
		t.Fatalf("failed upgrade left transient manifest: %v", err)
	}
	if temps := review141TempPaths(box.appHome, filepath.Dir(box.binPath)); len(temps) != 0 {
		t.Fatalf("failed upgrade left temps: %s", strings.Join(temps, ", "))
	}
	box.assertOwnedPaths()

	removed := box.run("uninstall", 0, 0)
	if removed.code != 0 {
		t.Fatalf("uninstall after failed upgrade exit %d, want 0\n%s", removed.code, removed.out)
	}
	box.assertUninstalled()
	box.keepsEqual()
}

// The upgrade aside path is mv dst->prev, then mv tmp->dst, then mv prev->dst
// on failure. box.run("install", 0, 2) lets the first two succeed and fails
// the replacement and the restore. The previous install must still be owned.
func TestReview141InstallUpgradeRestoreMvFailure(t *testing.T) {
	box := newReview141InstallBox(t)

	installed := box.run("install", 0, 0)
	if installed.code != 0 {
		t.Fatalf("seed install exit %d, want 0\n%s", installed.code, installed.out)
	}
	box.assertInstalled()
	manifest, err := os.ReadFile(box.manifest)
	if err != nil {
		t.Fatal(err)
	}

	failed := box.run("install", 0, 2)
	if failed.code == 0 {
		t.Fatalf("upgrade restore failure exited 0\n%s", failed.out)
	}
	box.keepsEqual()
	if got, err := os.ReadFile(box.manifest); err != nil || !bytes.Equal(got, manifest) {
		t.Fatalf("failed upgrade changed ownership manifest: %v\n%s", err, got)
	}
	box.assertOwnedPaths()

	removed := box.run("uninstall", 0, 0)
	if removed.code != 0 {
		t.Fatalf("uninstall after failed restore exit %d, want 0\n%s", removed.code, removed.out)
	}
	box.assertUninstalled()
	box.keepsEqual()
}

type review141InstallBox struct {
	t           *testing.T
	repo        string
	appHome     string
	binPath     string
	configDir   string
	pathDir     string
	bash        string
	manifest    string
	manifestNew string
	cpCount     string
	mvCount     string
	keeps       map[string]string
}

func newReview141InstallBox(t *testing.T) *review141InstallBox {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not available: %v", err)
	}
	root := t.TempDir()
	box := &review141InstallBox{
		t:         t,
		repo:      filepath.Join(root, "repo"),
		appHome:   filepath.Join(root, "app-home"),
		binPath:   filepath.Join(root, "install-bin", "usagebar"),
		configDir: filepath.Join(root, "config"),
		pathDir:   filepath.Join(root, "path"),
		bash:      bash,
		cpCount:   filepath.Join(root, "cp.count"),
		mvCount:   filepath.Join(root, "mv.count"),
	}
	box.manifest = filepath.Join(box.configDir, "herdr-app-install.manifest")
	box.manifestNew = filepath.Join(box.configDir, "herdr-app-install.manifest.new")
	for _, dir := range []string{
		filepath.Join(box.repo, "scripts"),
		filepath.Join(box.repo, "bin"),
		filepath.Join(box.repo, "app"),
		box.appHome,
		filepath.Dir(box.binPath),
		box.configDir,
		box.pathDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "herdr-app-install.sh"))
	if err != nil {
		t.Fatalf("read installer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(box.repo, "scripts", "herdr-app-install.sh"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	review141CopyDir(t, filepath.Join("..", "..", "app"), filepath.Join(box.repo, "app"))
	stub := "#!/bin/sh\nprintf 'review141-usagebar\\n' > \"$(dirname \"$0\")/usagebar\"\nchmod 755 \"$(dirname \"$0\")/usagebar\"\n"
	if err := os.WriteFile(filepath.Join(box.repo, "bin", "ensure-binary.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	box.keeps = map[string]string{}
	box.keep(filepath.Join(box.appHome, "keep-unrelated.txt"), "keep-unrelated\n")
	box.keep(filepath.Join(box.appHome, "mods", "personal-mod", "keep.txt"), "personal\n")
	box.keep(filepath.Join(box.appHome, "layouts", "personal.json"), "{}\n")
	box.keep(filepath.Join(filepath.Dir(box.binPath), "keep-bin.txt"), "keep-bin\n")
	box.keep(filepath.Join(box.configDir, "config.toml"), "# review141-config-marker\n[notify]\nenabled = false\n")
	box.keep(filepath.Join(box.configDir, "notes.txt"), "config-note\n")
	box.linkTools()
	box.writeShim("cp", box.cpCount, "REVIEW141_FAIL_CP_AFTER", "REVIEW141_REAL_CP")
	box.writeShim("mv", box.mvCount, "REVIEW141_FAIL_MV_AFTER", "REVIEW141_REAL_MV")
	if err := os.WriteFile(filepath.Join(box.pathDir, "herdr-app"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return box
}

func (box *review141InstallBox) linkTools() {
	box.t.Helper()
	for _, name := range []string{"basename", "bash", "cat", "chmod", "dirname", "find", "grep", "install", "mkdir", "rm", "sed"} {
		real, err := exec.LookPath(name)
		if err != nil {
			box.t.Fatalf("host tool %s: %v", name, err)
		}
		if err := os.Symlink(real, filepath.Join(box.pathDir, name)); err != nil {
			box.t.Fatalf("link %s: %v", name, err)
		}
	}
}

func (box *review141InstallBox) writeShim(name, countPath, limitEnv, realEnv string) {
	box.t.Helper()
	body := fmt.Sprintf(`#!/bin/sh
real=${%s:?}
count=%q
n=0
if [ -f "$count" ]; then
  n=$(cat "$count")
fi
n=$((n + 1))
printf '%%s\n' "$n" > "$count"
limit="${%s:-}"
if [ -n "$limit" ] && [ "$n" -gt "$limit" ]; then
  echo "review141: %s refused" >&2
  exit 1
fi
exec "$real" "$@"
`, realEnv, countPath, limitEnv, name)
	if err := os.WriteFile(filepath.Join(box.pathDir, name), []byte(body), 0o755); err != nil {
		box.t.Fatal(err)
	}
}

type review141ScriptResult struct {
	code int
	out  string
}

func (box *review141InstallBox) run(command string, failCP, failMV int) review141ScriptResult {
	box.t.Helper()
	if err := os.WriteFile(box.cpCount, []byte("0\n"), 0o644); err != nil {
		box.t.Fatal(err)
	}
	if err := os.WriteFile(box.mvCount, []byte("0\n"), 0o644); err != nil {
		box.t.Fatal(err)
	}
	realCP, err := exec.LookPath("cp")
	if err != nil {
		box.t.Fatal(err)
	}
	realMV, err := exec.LookPath("mv")
	if err != nil {
		box.t.Fatal(err)
	}
	env := []string{
		"PATH=" + box.pathDir,
		"HOME=" + filepath.Join(box.repo, "home"),
		"TMPDIR=" + filepath.Join(box.repo, "tmp"),
		"HERDR_APP_HOME=" + box.appHome,
		"AGENT_USAGE_BIN=" + box.binPath,
		"AGENT_USAGE_CONFIG_DIR=" + box.configDir,
		"REVIEW141_REAL_CP=" + realCP,
		"REVIEW141_REAL_MV=" + realMV,
	}
	if failCP > 0 {
		env = append(env, fmt.Sprintf("REVIEW141_FAIL_CP_AFTER=%d", failCP))
	}
	if failMV > 0 {
		env = append(env, fmt.Sprintf("REVIEW141_FAIL_MV_AFTER=%d", failMV))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	script := filepath.Join(box.repo, "scripts", "herdr-app-install.sh")
	cmd := exec.CommandContext(ctx, box.bash, script, command)
	cmd.Dir = box.repo
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		box.t.Fatalf("installer %s timed out\n%s", command, out)
	}
	if err == nil {
		return review141ScriptResult{code: 0, out: string(out)}
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		box.t.Fatalf("run installer: %v\n%s", err, out)
	}
	return review141ScriptResult{code: exitErr.ExitCode(), out: string(out)}
}

func (box *review141InstallBox) keep(path, body string) {
	box.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		box.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		box.t.Fatal(err)
	}
	box.keeps[path] = body
}

func (box *review141InstallBox) keepsEqual() {
	box.t.Helper()
	for path, want := range box.keeps {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			box.t.Errorf("%s = %q, %v; want %q", path, got, err, want)
		}
	}
}

func (box *review141InstallBox) installedPaths() []string {
	return []string{
		box.binPath,
		filepath.Join(box.appHome, "mods", "agent-usage"),
		filepath.Join(box.appHome, "mods", "agent-usage-tabs"),
		filepath.Join(box.appHome, "layouts", "agent-usage.json"),
	}
}

func (box *review141InstallBox) ownedLeftovers() []string {
	box.t.Helper()
	var bad []string
	for _, path := range append(box.installedPaths(), box.manifest, box.manifestNew) {
		if _, err := os.Stat(path); err == nil {
			bad = append(bad, path)
		}
	}
	return append(bad, review141TempPaths(box.appHome, filepath.Dir(box.binPath))...)
}

func (box *review141InstallBox) assertOwnedPaths() {
	box.t.Helper()
	for _, path := range box.installedPaths() {
		if _, err := os.Stat(path); err != nil {
			box.t.Fatalf("owned path missing after failed upgrade: %s: %v", path, err)
		}
	}
}

func (box *review141InstallBox) assertInstalled() {
	box.t.Helper()
	for _, path := range box.installedPaths() {
		if _, err := os.Stat(path); err != nil {
			box.t.Fatalf("installed path missing: %s: %v", path, err)
		}
	}
	data, err := os.ReadFile(box.binPath)
	if err != nil {
		box.t.Fatal(err)
	}
	if string(data) != "review141-usagebar\n" {
		box.t.Fatalf("binary = %q, want the stub payload", data)
	}
	local, err := os.ReadFile(filepath.Join(box.appHome, "mods", "agent-usage", "local.json"))
	if err != nil {
		box.t.Fatalf("local.json missing: %v", err)
	}
	if !bytes.Contains(local, []byte(box.binPath)) || !bytes.Contains(local, []byte(box.configDir)) {
		box.t.Fatalf("local.json = %s, want binary and config paths", local)
	}
	manifest, err := os.ReadFile(box.manifest)
	if err != nil {
		box.t.Fatalf("manifest missing: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(manifest), "\n"), "\n")
	have := map[string]bool{}
	for _, line := range lines {
		have[line] = true
	}
	for _, path := range box.installedPaths() {
		if !have[path] {
			box.t.Fatalf("manifest missing %s\n%s", path, manifest)
		}
	}
	for path := range box.keeps {
		if have[path] {
			box.t.Fatalf("manifest adopted unrelated path %s\n%s", path, manifest)
		}
	}
	if _, err := os.Stat(box.manifestNew); !os.IsNotExist(err) {
		box.t.Fatalf("successful install left transient manifest: %v", err)
	}
}

func (box *review141InstallBox) assertUninstalled() {
	box.t.Helper()
	for _, path := range append(box.installedPaths(), box.manifest, box.manifestNew) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			box.t.Fatalf("uninstall left %s: %v", path, err)
		}
	}
	if temps := review141TempPaths(box.appHome, filepath.Dir(box.binPath)); len(temps) != 0 {
		box.t.Fatalf("uninstall left temps: %s", strings.Join(temps, ", "))
	}
}

func review141TempPaths(roots ...string) []string {
	var out []string
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if strings.Contains(d.Name(), ".tmp.") {
				out = append(out, path)
			}
			return nil
		})
	}
	return out
}

func review141CopyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
