//go:build windows

package fsutil

import (
	"errors"
	"os"
	"syscall"
	"time"
)

const (
	// errorSharingViolation is ERROR_SHARING_VIOLATION: the file is open by
	// another process in a mode that forbids this access.
	errorSharingViolation syscall.Errno = 32

	// transientAttempts and transientBackoff bound the total wait to roughly
	// 1.3s. A competing reader or writer holds these small files for
	// microseconds, so a longer conflict means something else holds the file
	// and the error is reported instead.
	transientAttempts = 8
	transientBackoff  = 10 * time.Millisecond
)

// retryTransient runs op, retrying with doubling backoff while it fails with a
// sharing violation or access denied. Windows reports both while another
// process has the file open during a rename.
func retryTransient(op func() error) error {
	backoff := transientBackoff
	var err error
	for attempt := 0; attempt < transientAttempts; attempt++ {
		if err = op(); err == nil || !isTransientSharingError(err) {
			return err
		}
		time.Sleep(backoff)
		backoff *= 2
	}
	return err
}

func isTransientSharingError(err error) bool {
	return errors.Is(err, errorSharingViolation) || errors.Is(err, syscall.ERROR_ACCESS_DENIED)
}

// openShared opens path for reading with read, write and delete sharing, so a
// writer can rename a new version over it while this handle is still open.
func openShared(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := syscall.CreateFile(
		name,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}
