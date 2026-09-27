//go:build !windows

package fsutil

import "os"

// retryTransient runs op once: Unix renames and reads have no transient
// sharing conflicts to wait out.
func retryTransient(op func() error) error {
	return op()
}

// openShared opens path for reading. Unix opens never block a rename.
func openShared(path string) (*os.File, error) {
	return os.Open(path)
}

// renameReplacing renames src over dst. Unix rename already replaces dst
// atomically while readers hold it open.
func renameReplacing(src, dst string) error {
	return os.Rename(src, dst)
}
