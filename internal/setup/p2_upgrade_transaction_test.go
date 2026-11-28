package setup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type p2UpgradeEntry struct {
	mode os.FileMode
	data string
}

func p2UpgradeSnapshot(t *testing.T, roots ...string) map[string]p2UpgradeEntry {
	t.Helper()
	entries := make(map[string]p2UpgradeEntry)
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if path == root && os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			entry := p2UpgradeEntry{mode: info.Mode()}
			if info.Mode().IsRegular() {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				entry.data = string(data)
			} else if info.Mode()&os.ModeSymlink != 0 {
				entry.data, err = os.Readlink(path)
				if err != nil {
					return err
				}
			}
			entries[path] = entry
			return nil
		})
		if err != nil {
			t.Fatalf("snapshot %s: %v", root, err)
		}
	}
	return entries
}

func p2UpgradeWrite(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func p2UpgradeBinary(t *testing.T, box *review141InstallBox, version string) {
	t.Helper()
	stub := fmt.Sprintf("#!/bin/sh\nout=\"${2:-$(dirname \"$0\")/usagebar}\"\nprintf '%%s\\n' %q > \"$out\"\nchmod 755 \"$out\"\n", version)
	p2UpgradeWrite(t, filepath.Join(box.repo, "bin", "ensure-binary.sh"), stub, 0o755)
}

func p2UpgradeSources(t *testing.T, box *review141InstallBox, version string) {
	t.Helper()
	for _, mod := range []string{"agent-usage", "agent-usage-tabs"} {
		p2UpgradeWrite(t, filepath.Join(box.repo, "app", "mods", mod, "transaction-marker.txt"), version+"-"+mod+"\n", 0o640)
	}
	p2UpgradeWrite(t, filepath.Join(box.repo, "app", "layouts", "agent-usage.json"), fmt.Sprintf("{\"version\":%q}\n", version), 0o640)
	p2UpgradeBinary(t, box, version+"-binary")
}

// Inject failures by target and phase, not by the number of cp or mv calls.
func p2UpgradeShims(t *testing.T, box *review141InstallBox, phase string) (string, string) {
	t.Helper()
	marker := filepath.Join(box.repo, "failure-fired")
	published := filepath.Join(box.repo, "published-at-failure")
	checked := filepath.Join(box.repo, "check-reached")
	mod := filepath.Join(box.appHome, "mods", "agent-usage")
	tabs := filepath.Join(box.appHome, "mods", "agent-usage-tabs")
	layout := filepath.Join(box.appHome, "layouts", "agent-usage.json")
	newPublished := fmt.Sprintf(`{ [ "$(cat %q 2>/dev/null)" = new-binary ] || [ "$(cat %q 2>/dev/null)" = new-agent-usage ] || [ "$(cat %q 2>/dev/null)" = new-agent-usage-tabs ] || [ "$(cat %q 2>/dev/null)" = '{"version":"new"}' ]; }`, box.binPath, filepath.Join(mod, "transaction-marker.txt"), filepath.Join(tabs, "transaction-marker.txt"), layout)
	allPublished := fmt.Sprintf(`[ "$(cat %q 2>/dev/null)" = new-binary ] && [ "$(cat %q 2>/dev/null)" = new-agent-usage ] && [ "$(cat %q 2>/dev/null)" = new-agent-usage-tabs ] && [ "$(cat %q 2>/dev/null)" = '{"version":"new"}' ] && [ -f %q ] && [ "$(cat %q 2>/dev/null)" != '{"version":"old-local-json"}' ]`, box.binPath, filepath.Join(mod, "transaction-marker.txt"), filepath.Join(tabs, "transaction-marker.txt"), layout, filepath.Join(mod, "local.json"), filepath.Join(mod, "local.json"))
	cp := fmt.Sprintf(`#!/bin/sh
for arg do
  if [ %q = restore-blocked ] && [ -e %q ] && [ "$(cat "$arg/transaction-marker.txt" 2>/dev/null)" = old-agent-usage ]; then
    printf 'copy\n' > %q
    exit 1
  fi
  case "$arg" in
    %q|%q|%q)
      if [ %q = staging ] && [ ! -e %q ]; then
        printf 'staging\n' > %q
        exit 1
      fi ;;
  esac
done
exec "$REVIEW141_REAL_CP" "$@"
`, phase, marker, filepath.Join(box.repo, "copy-refused"), filepath.Join(box.repo, "app", "mods", "agent-usage-tabs"), filepath.Join(box.repo, "app", "mods", "agent-usage-tabs")+"/", filepath.Join(box.repo, "app", "mods", "agent-usage-tabs")+"/.", phase, marker, marker)
	mv := fmt.Sprintf(`#!/bin/sh
src= dst=
for arg do
  case "$arg" in -*) continue ;; esac
  src=$dst
  dst=$arg
done
if [ %q = publish ] && [ ! -e %q ]; then
  case "$dst" in
    %q|%q|%q|%q)
      if %s; then
        printf 'publish\n' > %q
        printf 'observed\n' > %q
        exit 1
      fi ;;
  esac
fi
if [ %q = manifest ] && [ "$dst" = %q ] && [ ! -e %q ] && [ -e %q ] && %s; then
  printf 'manifest\n' > %q
  exit 1
fi
if { [ %q = restore ] || [ %q = restore-blocked ]; } && [ -e %q ] && [ "$dst" = %q ] && [ "$(cat "$src/transaction-marker.txt" 2>/dev/null)" = old-agent-usage ]; then
  printf 'restore\n' > %q
  exit 1
fi
exec "$REVIEW141_REAL_MV" "$@"
`, phase, marker, box.binPath, mod, tabs, layout, newPublished, marker, published,
		phase, box.manifest, marker, checked, allPublished, marker,
		phase, phase, marker, mod, filepath.Join(box.repo, "restore-refused"))
	check := fmt.Sprintf(`#!/bin/sh
if [ "$1" = check ]; then
  if %s; then printf 'published\n' > %q; fi
  if [ -f %q ]; then printf 'new target published\n' > %q; fi
  if [ %q = check ] || [ %q = restore ] || [ %q = new-target ] || [ %q = restore-blocked ]; then
    printf 'check\n' > %q
    exit 1
  fi
fi
exit 0
`, allPublished, checked, filepath.Join(box.appHome, "mods", "agent-usage-new", "index.js"), filepath.Join(box.repo, "new-target-published"), phase, phase, phase, phase, marker)
	p2UpgradeWrite(t, filepath.Join(box.pathDir, "cp"), cp, 0o755)
	p2UpgradeWrite(t, filepath.Join(box.pathDir, "mv"), mv, 0o755)
	p2UpgradeWrite(t, filepath.Join(box.pathDir, "herdr-app"), check, 0o755)
	return marker, published
}

func TestP2UpgradeTransactionPreservesPreviousInstall(t *testing.T) {
	for _, phase := range []string{"staging", "publish", "check", "manifest", "restore", "new-target"} {
		t.Run(phase, func(t *testing.T) {
			box := newReview141InstallBox(t)
			if runtime.GOOS == "darwin" {
				box.bash = "/bin/bash"
			}
			p2UpgradeSources(t, box, "old")
			seed := box.run("install", 0, 0)
			if seed.code != 0 {
				t.Fatalf("seed install failed: %s", seed.out)
			}
			box.assertOwnedPaths()
			manifest, err := os.ReadFile(box.manifest)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSuffix(string(manifest), "\n"), "\n")
			if len(lines) != len(box.installedPaths()) {
				t.Fatalf("seed manifest does not contain every owned target: %s", manifest)
			}
			for _, path := range box.installedPaths() {
				if !strings.Contains("\n"+string(manifest), "\n"+path+"\n") {
					t.Fatalf("seed manifest missing %s: %s", path, manifest)
				}
			}
			mod := filepath.Join(box.appHome, "mods", "agent-usage")
			p2UpgradeWrite(t, filepath.Join(mod, "local.json"), "{\"version\":\"old-local-json\"}\n", 0o600)
			p2UpgradeWrite(t, filepath.Join(mod, "old-only", "nested.txt"), "old nested data\n", 0o600)
			p2UpgradeWrite(t, filepath.Join(box.appHome, "mods", "agent-usage-tabs", "old-only.txt"), "old tabs data\n", 0o640)
			if err := os.Chmod(box.binPath, 0o701); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(box.appHome, "layouts", "agent-usage.json"), 0o600); err != nil {
				t.Fatal(err)
			}
			roots := append(box.installedPaths(), box.manifest)
			before := p2UpgradeSnapshot(t, roots...)
			p2UpgradeSources(t, box, "new")
			newTarget := filepath.Join(box.appHome, "mods", "agent-usage-new")
			if phase == "new-target" {
				p2UpgradeWrite(t, filepath.Join(box.repo, "app", "mods", "agent-usage-new", "index.js"), "new mod\n", 0o644)
			}
			marker, published := p2UpgradeShims(t, box, phase)
			failed := box.run("install", 0, 0)
			if failed.code == 0 {
				t.Errorf("upgrade unexpectedly succeeded\n%s", failed.out)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("failure injection did not reach %s: %v\n%s", phase, err, failed.out)
			}
			if phase == "publish" {
				if _, err := os.Stat(published); err != nil {
					t.Errorf("publish failure did not follow a published target: %v", err)
				}
			}
			if phase == "check" || phase == "manifest" || phase == "restore" || phase == "new-target" {
				if _, err := os.Stat(filepath.Join(box.repo, "check-reached")); err != nil {
					t.Errorf("check did not observe all new published targets: %v\n%s", err, failed.out)
				}
			}
			if phase == "restore" {
				if _, err := os.Stat(filepath.Join(box.repo, "restore-refused")); err != nil {
					t.Errorf("rollback rename was not refused: %v\n%s", err, failed.out)
				}
			}
			if phase == "new-target" {
				if _, err := os.Stat(filepath.Join(box.repo, "new-target-published")); err != nil {
					t.Errorf("check did not observe the newly introduced target: %v", err)
				}
				if _, err := os.Lstat(newTarget); !os.IsNotExist(err) {
					t.Errorf("new target survived failed upgrade: %v", err)
				}
			}
			box.keepsEqual()
			after := p2UpgradeSnapshot(t, roots...)
			if !reflect.DeepEqual(before, after) {
				for path, want := range before {
					if got, ok := after[path]; !ok || got != want {
						t.Errorf("previous target changed: %s: present=%t, mode=%v (want %v), bytes=%d (want %d)", path, ok, got.mode, want.mode, len(got.data), len(want.data))
					}
				}
				for path := range after {
					if _, ok := before[path]; !ok {
						t.Errorf("unexpected entry after rollback: %s", path)
					}
				}
			}
			for _, root := range []string{box.appHome, filepath.Dir(box.binPath), box.configDir} {
				err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if strings.Contains(d.Name(), ".tmp.") || strings.Contains(d.Name(), ".prev.") || path == box.manifestNew {
						t.Errorf("failed upgrade left disposable staging or backup: %s", path)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			box.writeShim("cp", box.cpCount, "REVIEW141_FAIL_CP_AFTER", "REVIEW141_REAL_CP")
			box.writeShim("mv", box.mvCount, "REVIEW141_FAIL_MV_AFTER", "REVIEW141_REAL_MV")
			p2UpgradeWrite(t, filepath.Join(box.pathDir, "herdr-app"), "#!/bin/sh\nexit 0\n", 0o755)
			retry := box.run("install", 0, 0)
			if retry.code != 0 {
				t.Fatalf("retry failed: %s", retry.out)
			}
			removed := box.run("uninstall", 0, 0)
			if removed.code != 0 {
				t.Fatalf("uninstall failed: %s", removed.out)
			}
			box.assertUninstalled()
			if phase == "new-target" {
				if _, err := os.Lstat(newTarget); !os.IsNotExist(err) {
					t.Errorf("uninstall left new target: %v", err)
				}
			}
			box.keepsEqual()
		})
	}
}

func TestP2UpgradeFirstInstallWithoutBackups(t *testing.T) {
	box := newReview141InstallBox(t)
	if runtime.GOOS == "darwin" {
		box.bash = "/bin/bash"
	}
	p2UpgradeSources(t, box, "new")
	marker, _ := p2UpgradeShims(t, box, "check")
	failed := box.run("install", 0, 0)
	if failed.code == 0 {
		t.Errorf("first install unexpectedly succeeded\n%s", failed.out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("first install did not reach the failing check: %v\n%s", err, failed.out)
	}
	if _, err := os.Stat(filepath.Join(box.repo, "check-reached")); err != nil {
		t.Errorf("check did not observe the published first install: %v\n%s", err, failed.out)
	}
	if leftovers := box.ownedLeftovers(); len(leftovers) != 0 {
		t.Errorf("failed first install left paths: %s", strings.Join(leftovers, ", "))
	}
	box.keepsEqual()
	box.writeShim("cp", box.cpCount, "REVIEW141_FAIL_CP_AFTER", "REVIEW141_REAL_CP")
	box.writeShim("mv", box.mvCount, "REVIEW141_FAIL_MV_AFTER", "REVIEW141_REAL_MV")
	p2UpgradeWrite(t, filepath.Join(box.pathDir, "herdr-app"), "#!/bin/sh\nexit 0\n", 0o755)
	retry := box.run("install", 0, 0)
	if retry.code != 0 {
		t.Fatalf("first install retry failed: %s", retry.out)
	}
	box.assertOwnedPaths()
	removed := box.run("uninstall", 0, 0)
	if removed.code != 0 {
		t.Fatalf("uninstall after retry failed: %s", removed.out)
	}
	box.assertUninstalled()
	box.keepsEqual()
}

func TestP2UpgradeUnrecoverableRollbackKeepsBackup(t *testing.T) {
	box := newReview141InstallBox(t)
	if runtime.GOOS == "darwin" {
		box.bash = "/bin/bash"
	}
	p2UpgradeSources(t, box, "old")
	seed := box.run("install", 0, 0)
	if seed.code != 0 {
		t.Fatalf("seed install failed: %s", seed.out)
	}
	mod := filepath.Join(box.appHome, "mods", "agent-usage")
	p2UpgradeWrite(t, filepath.Join(mod, "old-only", "nested.txt"), "irreplaceable old data\n", 0o600)
	p2UpgradeWrite(t, filepath.Join(mod, "local.json"), "{\"version\":\"old-local-json\"}\n", 0o600)
	before := p2UpgradeSnapshot(t, mod)
	manifest, err := os.ReadFile(box.manifest)
	if err != nil {
		t.Fatal(err)
	}
	p2UpgradeSources(t, box, "new")
	marker, _ := p2UpgradeShims(t, box, "restore-blocked")
	failed := box.run("install", 0, 0)
	if failed.code == 0 {
		t.Errorf("upgrade unexpectedly succeeded\n%s", failed.out)
	}
	for _, path := range []string{marker, filepath.Join(box.repo, "check-reached"), filepath.Join(box.repo, "restore-refused"), filepath.Join(box.repo, "copy-refused")} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("failure phase was not reached (%s): %v\n%s", path, err, failed.out)
		}
	}
	if got, err := os.ReadFile(box.manifest); err != nil || string(got) != string(manifest) {
		t.Errorf("failed upgrade changed manifest: %v", err)
	}
	box.keepsEqual()
	var backup string
	err = filepath.WalkDir(filepath.Join(box.appHome, "mods"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != mod {
			data, readErr := os.ReadFile(filepath.Join(path, "old-only", "nested.txt"))
			if readErr == nil && string(data) == "irreplaceable old data\n" {
				backup = path
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatalf("old mod backup was not preserved\n%s", failed.out)
	}
	if !strings.Contains(failed.out, backup) {
		t.Errorf("recovery path %s not reported\n%s", backup, failed.out)
	}
	after := p2UpgradeSnapshot(t, backup)
	for path, want := range before {
		rel, err := filepath.Rel(mod, path)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := after[filepath.Join(backup, rel)]; !ok || got != want {
			t.Errorf("backup entry %s not preserved: present=%t mode=%v want=%v", rel, ok, got.mode, want.mode)
		}
	}
}

func TestP2UpgradeBinaryIsInTreeBuildTarget(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "first install"
		if upgrade {
			name = "upgrade"
		}
		t.Run(name, func(t *testing.T) {
			box := newReview141InstallBox(t)
			if runtime.GOOS == "darwin" {
				box.bash = "/bin/bash"
			}
			box.binPath = filepath.Join(box.repo, "bin", "usagebar")
			var before map[string]p2UpgradeEntry
			if upgrade {
				p2UpgradeSources(t, box, "old")
				seed := box.run("install", 0, 0)
				if seed.code != 0 {
					t.Fatalf("seed install failed: %s", seed.out)
				}
				if err := os.Chmod(box.binPath, 0o701); err != nil {
					t.Fatal(err)
				}
				p2UpgradeWrite(t, filepath.Join(box.appHome, "mods", "agent-usage", "local.json"), "{\"version\":\"old-local-json\"}\n", 0o600)
				before = p2UpgradeSnapshot(t, append(box.installedPaths(), box.manifest)...)
			}
			p2UpgradeSources(t, box, "new")
			marker, _ := p2UpgradeShims(t, box, "check")
			failed := box.run("install", 0, 0)
			if failed.code == 0 {
				t.Errorf("install unexpectedly succeeded\n%s", failed.out)
			}
			for _, path := range []string{marker, filepath.Join(box.repo, "check-reached")} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("published install did not reach failing check: %v\n%s", err, failed.out)
				}
			}
			if upgrade {
				after := p2UpgradeSnapshot(t, append(box.installedPaths(), box.manifest)...)
				if !reflect.DeepEqual(before, after) {
					for path, want := range before {
						if got, ok := after[path]; !ok || got != want {
							t.Errorf("previous target changed: %s: present=%t mode=%v want=%v", path, ok, got.mode, want.mode)
						}
					}
					for path := range after {
						if _, ok := before[path]; !ok {
							t.Errorf("unexpected target after rollback: %s", path)
						}
					}
				}
			} else if leftovers := box.ownedLeftovers(); len(leftovers) != 0 {
				t.Errorf("failed first install left paths: %s", strings.Join(leftovers, ", "))
			}
			box.keepsEqual()
			box.writeShim("cp", box.cpCount, "REVIEW141_FAIL_CP_AFTER", "REVIEW141_REAL_CP")
			box.writeShim("mv", box.mvCount, "REVIEW141_FAIL_MV_AFTER", "REVIEW141_REAL_MV")
			p2UpgradeWrite(t, filepath.Join(box.pathDir, "herdr-app"), "#!/bin/sh\nexit 0\n", 0o755)
			retry := box.run("install", 0, 0)
			if retry.code != 0 {
				t.Fatalf("retry failed: %s", retry.out)
			}
			removed := box.run("uninstall", 0, 0)
			if removed.code != 0 {
				t.Fatalf("uninstall failed: %s", removed.out)
			}
			box.assertUninstalled()
			box.keepsEqual()
		})
	}
}

func TestP2UpgradeRegularFilesRemainAvailableUntilReplacement(t *testing.T) {
	box := newReview141InstallBox(t)
	if runtime.GOOS == "darwin" {
		box.bash = "/bin/bash"
	}
	p2UpgradeSources(t, box, "old")
	seed := box.run("install", 0, 0)
	if seed.code != 0 {
		t.Fatalf("seed install failed: %s", seed.out)
	}
	layout := filepath.Join(box.appHome, "layouts", "agent-usage.json")
	binaryObserved := filepath.Join(box.repo, "binary-at-replacement")
	layoutObserved := filepath.Join(box.repo, "layout-at-replacement")
	p2UpgradeSources(t, box, "new")
	shim := fmt.Sprintf(`#!/bin/sh
src= dst=
for arg do
  case "$arg" in -*) continue ;; esac
  src=$dst
  dst=$arg
done
case "$dst" in
  %q) observed=%q ;;
  %q) observed=%q ;;
  *) observed= ;;
esac
if [ -n "$observed" ]; then
  case "$src" in
    "$dst".tmp.*)
      if [ -f "$dst" ]; then cat "$dst" > "$observed"; else printf 'MISSING\n' > "$observed"; fi ;;
  esac
fi
exec "$REVIEW141_REAL_MV" "$@"
`, box.binPath, binaryObserved, layout, layoutObserved)
	p2UpgradeWrite(t, filepath.Join(box.pathDir, "mv"), shim, 0o755)
	upgrade := box.run("install", 0, 0)
	if upgrade.code != 0 {
		t.Fatalf("upgrade failed: %s", upgrade.out)
	}
	for path, want := range map[string]string{
		binaryObserved: "old-binary\n",
		layoutObserved: "{\"version\":\"old\"}\n",
		box.binPath:    "new-binary\n",
		layout:         "{\"version\":\"new\"}\n",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", path, got, err, want)
		}
	}
	box.keepsEqual()
	removed := box.run("uninstall", 0, 0)
	if removed.code != 0 {
		t.Fatalf("uninstall failed: %s", removed.out)
	}
	box.assertUninstalled()
	box.keepsEqual()
}

func TestP2UpgradeBackupCopyFailurePreservesPreviousInstall(t *testing.T) {
	box := newReview141InstallBox(t)
	if runtime.GOOS == "darwin" {
		box.bash = "/bin/bash"
	}
	p2UpgradeSources(t, box, "old")
	seed := box.run("install", 0, 0)
	if seed.code != 0 {
		t.Fatalf("seed install failed: %s", seed.out)
	}
	layout := filepath.Join(box.appHome, "layouts", "agent-usage.json")
	p2UpgradeWrite(t, filepath.Join(box.appHome, "mods", "agent-usage", "local.json"), "{\"version\":\"old-local-json\"}\n", 0o600)
	if err := os.Chmod(layout, 0o600); err != nil {
		t.Fatal(err)
	}
	roots := append(box.installedPaths(), box.manifest)
	before := p2UpgradeSnapshot(t, roots...)
	p2UpgradeSources(t, box, "new")
	marker := filepath.Join(box.repo, "backup-copy-failed")
	shim := fmt.Sprintf(`#!/bin/sh
src= dst=
for arg do
  case "$arg" in -*) continue ;; esac
  src=$dst
  dst=$arg
done
if [ "$src" = %q ] && [ ! -e %q ]; then
  printf 'partial backup\n' > "$dst"
  printf 'copy failed\n' > %q
  exit 1
fi
exec "$REVIEW141_REAL_CP" "$@"
`, layout, marker, marker)
	p2UpgradeWrite(t, filepath.Join(box.pathDir, "cp"), shim, 0o755)
	failed := box.run("install", 0, 0)
	if failed.code == 0 {
		t.Errorf("upgrade unexpectedly succeeded\n%s", failed.out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("layout backup copy did not fail: %v\n%s", err, failed.out)
	}
	after := p2UpgradeSnapshot(t, roots...)
	if !reflect.DeepEqual(before, after) {
		for path, want := range before {
			if got, ok := after[path]; !ok || got != want {
				t.Errorf("previous target changed: %s: present=%t mode=%v want=%v", path, ok, got.mode, want.mode)
			}
		}
		for path := range after {
			if _, ok := before[path]; !ok {
				t.Errorf("unexpected target after rollback: %s", path)
			}
		}
	}
	for _, root := range []string{box.appHome, filepath.Dir(box.binPath), box.configDir} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if strings.Contains(d.Name(), ".tmp.") || strings.Contains(d.Name(), ".prev.") || path == box.manifestNew {
				t.Errorf("failed upgrade left staging or backup: %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	box.keepsEqual()
	box.writeShim("cp", box.cpCount, "REVIEW141_FAIL_CP_AFTER", "REVIEW141_REAL_CP")
	retry := box.run("install", 0, 0)
	if retry.code != 0 {
		t.Fatalf("retry failed: %s", retry.out)
	}
	removed := box.run("uninstall", 0, 0)
	if removed.code != 0 {
		t.Fatalf("uninstall failed: %s", removed.out)
	}
	box.assertUninstalled()
	box.keepsEqual()
}

func TestP2UpgradeOwnedModSymlinkDoesNotModifyTarget(t *testing.T) {
	for _, targetKind := range []string{"directory", "file", "dangling"} {
		for _, failCheck := range []bool{false, true} {
			name := targetKind + "/success"
			if failCheck {
				name = targetKind + "/failed-check"
			}
			t.Run(name, func(t *testing.T) {
				box := newReview141InstallBox(t)
				if runtime.GOOS == "darwin" {
					box.bash = "/bin/bash"
				}
				p2UpgradeSources(t, box, "old")
				seed := box.run("install", 0, 0)
				if seed.code != 0 {
					t.Fatalf("seed install failed: %s", seed.out)
				}
				external := filepath.Join(box.repo, "external")
				p2UpgradeWrite(t, filepath.Join(external, "keep", "nested.txt"), "external data\n", 0o600)
				var target string
				switch targetKind {
				case "directory":
					target = filepath.Join(external, "keep")
				case "file":
					target = filepath.Join(external, "keep", "nested.txt")
				case "dangling":
					target = filepath.Join(external, "missing")
				}
				mod := filepath.Join(box.appHome, "mods", "agent-usage")
				if err := os.RemoveAll(mod); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, mod); err != nil {
					t.Fatal(err)
				}
				roots := append(box.installedPaths(), box.manifest)
				before := p2UpgradeSnapshot(t, roots...)
				externalBefore := p2UpgradeSnapshot(t, external)
				p2UpgradeSources(t, box, "new")
				var marker string
				if failCheck {
					marker, _ = p2UpgradeShims(t, box, "check")
				}
				upgrade := box.run("install", 0, 0)
				if failCheck {
					if upgrade.code == 0 {
						t.Errorf("upgrade unexpectedly succeeded\n%s", upgrade.out)
					}
					for _, path := range []string{marker, filepath.Join(box.repo, "check-reached")} {
						if _, err := os.Stat(path); err != nil {
							t.Fatalf("published upgrade did not reach failing check: %v\n%s", err, upgrade.out)
						}
					}
					after := p2UpgradeSnapshot(t, roots...)
					if !reflect.DeepEqual(before, after) {
						for path, want := range before {
							if got, ok := after[path]; !ok || got != want {
								t.Errorf("previous target changed: %s: present=%t mode=%v want=%v", path, ok, got.mode, want.mode)
							}
						}
						for path := range after {
							if _, ok := before[path]; !ok {
								t.Errorf("unexpected target after rollback: %s", path)
							}
						}
					}
					if got, err := os.Readlink(mod); err != nil || got != target {
						t.Errorf("restored mod link = %q, %v; want %q", got, err, target)
					}
				} else {
					if upgrade.code != 0 {
						t.Fatalf("upgrade failed: %s", upgrade.out)
					}
					info, err := os.Lstat(mod)
					if err != nil || !info.IsDir() {
						t.Fatalf("installed mod is not a real directory: %v, %v", info, err)
					}
					got, err := os.ReadFile(filepath.Join(mod, "transaction-marker.txt"))
					if err != nil || string(got) != "new-agent-usage\n" {
						t.Errorf("installed mod marker = %q, %v", got, err)
					}
				}
				if after := p2UpgradeSnapshot(t, external); !reflect.DeepEqual(externalBefore, after) {
					t.Errorf("external symlink target tree changed: before=%v after=%v", externalBefore, after)
				}
				box.keepsEqual()
				if failCheck {
					box.writeShim("cp", box.cpCount, "REVIEW141_FAIL_CP_AFTER", "REVIEW141_REAL_CP")
					box.writeShim("mv", box.mvCount, "REVIEW141_FAIL_MV_AFTER", "REVIEW141_REAL_MV")
					p2UpgradeWrite(t, filepath.Join(box.pathDir, "herdr-app"), "#!/bin/sh\nexit 0\n", 0o755)
					retry := box.run("install", 0, 0)
					if retry.code != 0 {
						t.Fatalf("retry failed: %s", retry.out)
					}
				}
				removed := box.run("uninstall", 0, 0)
				if removed.code != 0 {
					t.Fatalf("uninstall failed: %s", removed.out)
				}
				box.assertUninstalled()
				box.keepsEqual()
				if after := p2UpgradeSnapshot(t, external); !reflect.DeepEqual(externalBefore, after) {
					t.Errorf("retry or uninstall changed external tree: before=%v after=%v", externalBefore, after)
				}
			})
		}
	}
}

func TestP2UpgradeBinarySymlinkToDirectoryDoesNotModifyTarget(t *testing.T) {
	for _, failCheck := range []bool{false, true} {
		name := "success"
		if failCheck {
			name = "failed-check"
		}
		t.Run(name, func(t *testing.T) {
			box := newReview141InstallBox(t)
			if runtime.GOOS == "darwin" {
				box.bash = "/bin/bash"
			}
			p2UpgradeSources(t, box, "old")
			seed := box.run("install", 0, 0)
			if seed.code != 0 {
				t.Fatalf("seed install failed: %s", seed.out)
			}
			external := filepath.Join(box.repo, "external-bin")
			p2UpgradeWrite(t, filepath.Join(external, "keep.txt"), "external binary directory\n", 0o600)
			if err := os.Remove(box.binPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, box.binPath); err != nil {
				t.Fatal(err)
			}
			roots := append(box.installedPaths(), box.manifest)
			before := p2UpgradeSnapshot(t, roots...)
			externalBefore := p2UpgradeSnapshot(t, external)
			p2UpgradeSources(t, box, "new")
			var marker string
			if failCheck {
				marker, _ = p2UpgradeShims(t, box, "check")
			}
			upgrade := box.run("install", 0, 0)
			if failCheck {
				if upgrade.code == 0 {
					t.Errorf("upgrade unexpectedly succeeded\n%s", upgrade.out)
				}
				for _, path := range []string{marker, filepath.Join(box.repo, "check-reached")} {
					if _, err := os.Stat(path); err != nil {
						t.Fatalf("published upgrade did not reach failing check: %v\n%s", err, upgrade.out)
					}
				}
				after := p2UpgradeSnapshot(t, roots...)
				if !reflect.DeepEqual(before, after) {
					t.Errorf("old targets or manifest changed after rollback")
				}
				if got, err := os.Readlink(box.binPath); err != nil || got != external {
					t.Errorf("restored binary link = %q, %v; want %q", got, err, external)
				}
			} else {
				if upgrade.code != 0 {
					t.Fatalf("upgrade failed: %s", upgrade.out)
				}
				info, err := os.Lstat(box.binPath)
				if err != nil || !info.Mode().IsRegular() {
					t.Fatalf("installed binary is not a regular file: %v, %v", info, err)
				}
				got, err := os.ReadFile(box.binPath)
				if err != nil || string(got) != "new-binary\n" {
					t.Errorf("installed binary = %q, %v", got, err)
				}
			}
			if after := p2UpgradeSnapshot(t, external); !reflect.DeepEqual(externalBefore, after) {
				t.Errorf("external directory changed: before=%v after=%v", externalBefore, after)
			}
			box.keepsEqual()
		})
	}
}
