/**
 * Atomic file replacement and reads that tolerate a concurrent replacement.
 *
 * Every cache and snapshot in this plugin is written to a temp file and then
 * renamed over the destination so readers never see a torn write. On Unix that
 * rename is atomic even while readers hold the old file open. On Windows the
 * default rename (MoveFileEx) fails while the destination is open, and an open
 * issued during a rename can fail with a sharing violation. The Windows
 * variants therefore rename with POSIX semantics, open readers with delete
 * sharing so they never block that rename, and retry the brief transient
 * conflicts instead of dropping the write or reporting the file as unreadable.
 */
package fsutil

import "io"

// ReplaceFile atomically renames src over dst, retrying transient Windows
// sharing conflicts. src and dst must be on the same filesystem.
func ReplaceFile(src, dst string) error {
	return retryTransient(func() error { return renameReplacing(src, dst) })
}

// ReadFile reads path like os.ReadFile, but without blocking a concurrent
// ReplaceFile of the same path and retrying transient sharing conflicts.
func ReadFile(path string) ([]byte, error) {
	var raw []byte
	err := retryTransient(func() error {
		f, err := openShared(path)
		if err != nil {
			return err
		}
		defer f.Close()
		raw, err = io.ReadAll(f)
		return err
	})
	return raw, err
}
