//go:build unix

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// useUmask022 pins the process umask to the common 022 default so that the
// modes asserted below reflect what the helpers request, not the host's umask.
// The umask is process-wide, so tests using it must not run in parallel.
func useUmask022(t *testing.T) {
	t.Helper()
	prev := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(prev) })
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	return info.Mode()
}

// assertNoTempLeftovers fails when dir still holds a `.*.tmp` file from a write.
func assertNoTempLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".tmp") {
			t.Fatalf("temp file left behind: %s", name)
		}
	}
}

// TestWritePrivateFileNewFileAndDirModes checks that a new parent directory is
// 0700 and the new file is 0600.
func TestWritePrivateFileNewFileAndDirModes(t *testing.T) {
	useUmask022(t)
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "usage.json")

	if err := WritePrivateFile(path, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("WritePrivateFile: %v", err)
	}
	if got := modeOf(t, path).Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 0600", got)
	}
	if got := modeOf(t, dir).Perm(); got != 0o700 {
		t.Fatalf("new dir mode = %o, want 0700", got)
	}
}

// TestWritePrivateFileTightensExistingFile checks that replacing a permissive
// existing file yields an owner-only file with the new content.
func TestWritePrivateFileTightensExistingFile(t *testing.T) {
	useUmask022(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if got := modeOf(t, path).Perm(); got != 0o644 {
		t.Fatalf("seed mode = %o, want 0644", got)
	}

	if err := WritePrivateFile(path, []byte("new")); err != nil {
		t.Fatalf("WritePrivateFile: %v", err)
	}
	if got := modeOf(t, path).Perm(); got != 0o600 {
		t.Fatalf("file mode after replace = %o, want 0600", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(raw) != "new" {
		t.Fatalf("content = %q, want %q", raw, "new")
	}
}

// TestMkdirPrivateKeepsExistingDirMode checks that an existing directory keeps
// its 0755 mode instead of being tightened.
func TestMkdirPrivateKeepsExistingDirMode(t *testing.T) {
	useUmask022(t)
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	if err := MkdirPrivate(dir); err != nil {
		t.Fatalf("MkdirPrivate: %v", err)
	}
	if got := modeOf(t, dir).Perm(); got != 0o755 {
		t.Fatalf("existing dir mode = %o, want 0755", got)
	}
	if err := WritePrivateFile(filepath.Join(dir, "x.json"), []byte("{}")); err != nil {
		t.Fatalf("WritePrivateFile: %v", err)
	}
	if got := modeOf(t, dir).Perm(); got != 0o755 {
		t.Fatalf("existing dir mode after write = %o, want 0755", got)
	}
}

// TestWritePrivateFileLeavesNoTempFiles checks that a successful write leaves
// only the destination file in its directory.
func TestWritePrivateFileLeavesNoTempFiles(t *testing.T) {
	useUmask022(t)
	dir := t.TempDir()
	if err := WritePrivateFile(filepath.Join(dir, "usage.json"), []byte("{}")); err != nil {
		t.Fatalf("WritePrivateFile: %v", err)
	}
	assertNoTempLeftovers(t, dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir holds %d entries, want 1", len(entries))
	}
}

// TestWritePrivateFileReplacesSymlinkWithoutTouchingTarget checks that a
// symlink at path is replaced by a regular file and its target is untouched.
func TestWritePrivateFileReplacesSymlinkWithoutTouchingTarget(t *testing.T) {
	useUmask022(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	link := filepath.Join(dir, "state.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("seed symlink: %v", err)
	}

	if err := WritePrivateFile(link, []byte("payload")); err != nil {
		t.Fatalf("WritePrivateFile: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("path is %v, want a regular file replacing the symlink", info.Mode().Type())
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 0600", got)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(raw) != "original" {
		t.Fatalf("symlink target modified: %q", raw)
	}
}

// TestWritePrivateFileConcurrentWritersAgree checks that concurrent writers to
// one path all succeed, end on exactly one complete payload, and leave no temp
// files behind.
func TestWritePrivateFileConcurrentWritersAgree(t *testing.T) {
	useUmask022(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.json")

	const writers = 16
	payloads := make([]string, writers)
	for i := range payloads {
		payloads[i] = fmt.Sprintf(`{"writer":%d,"pad":"%s"}`, i, strings.Repeat("x", 4096))
	}

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = WritePrivateFile(path, []byte(payloads[i]))
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	matched := false
	for _, p := range payloads {
		if string(raw) == p {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("final content is not one complete payload (len %d)", len(raw))
	}
	assertNoTempLeftovers(t, dir)
}

// TestOpenPrivateExclusiveCreatesOwnerOnlyLock checks that a new lock file is
// 0600 and that a second exclusive open fails with os.ErrExist.
func TestOpenPrivateExclusiveCreatesOwnerOnlyLock(t *testing.T) {
	useUmask022(t)
	path := filepath.Join(t.TempDir(), "state.lock")

	f, err := OpenPrivateExclusive(path)
	if err != nil {
		t.Fatalf("first OpenPrivateExclusive: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := modeOf(t, path).Perm(); got != 0o600 {
		t.Fatalf("lock mode = %o, want 0600", got)
	}

	if _, err := OpenPrivateExclusive(path); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second OpenPrivateExclusive err = %v, want os.ErrExist", err)
	}
}
