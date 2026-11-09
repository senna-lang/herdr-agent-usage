/**
 * Owner-only filesystem helpers for plugin-owned state: directories and
 * files that hold usage snapshots, caches, heartbeats, and lock files.
 *
 * Every plugin-owned state write goes through this file so the permission
 * policy lives in exactly one place.
 */
package fsutil

import (
	"os"
	"path/filepath"
)

const (
	// PrivateDirMode is the mode for dedicated plugin state directories.
	PrivateDirMode os.FileMode = 0o700
	// PrivateFileMode is the mode for plugin-owned state files and lock files.
	PrivateFileMode os.FileMode = 0o600
)

// MkdirPrivate creates dir and any missing parents with PrivateDirMode.
// Directories that already exist are left untouched: their mode is never
// changed, so a user-managed directory keeps its permissions.
func MkdirPrivate(dir string) error {
	return os.MkdirAll(dir, PrivateDirMode)
}

// WritePrivateFile atomically replaces path with data as an owner-only file.
//
// The parent directory is created with MkdirPrivate if missing. Data goes to
// a uniquely named temp file in the same directory (created 0600 regardless
// of umask), which is then renamed onto path. Two consequences follow from
// the rename, and callers rely on both:
//   - An existing permissive regular file at path becomes owner-only on the
//     next write, without any chmod of the old file.
//   - A symlink at path is replaced by a regular file rather than followed,
//     so the link's target is never modified.
//
// The temp file is removed on every failure path. A crash mid-write leaves
// at most a dot-prefixed `.*.tmp` file, never a truncated path.
func WritePrivateFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := MkdirPrivate(dir); err != nil {
		return err
	}
	// Same directory as the destination so the rename cannot cross filesystems.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// No-op once the rename succeeded; removes the temp file on any
		// failure path.
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// OpenPrivateExclusive creates path with PrivateFileMode, failing with an
// error wrapping os.ErrExist when it already exists. It is the primitive
// behind O_EXCL lock files, so every lock shares the same mode.
func OpenPrivateExclusive(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, PrivateFileMode)
}
