//go:build windows

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileRenameInfo mirrors FILE_RENAME_INFO with the Flags member of its union.
// Go's natural alignment reproduces the C layout on every Windows target.
type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

// renameReplacing renames src over dst with POSIX semantics: the name switches
// atomically even while other processes hold dst open, and those handles keep
// reading the old content, exactly as a Unix rename behaves. os.Rename
// (MoveFileEx) instead fails with access denied whenever dst is open. POSIX
// semantics need Windows 10 1607+ and NTFS; elsewhere this falls back to
// os.Rename.
func renameReplacing(src, dst string) error {
	err := posixRename(src, dst)
	if err == nil {
		return nil
	}
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED) ||
		errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		return os.Rename(src, dst)
	}
	return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
}

func posixRename(src, dst string) error {
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	srcName, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	dstName, err := windows.UTF16FromString(absDst)
	if err != nil {
		return err
	}

	handle, err := windows.CreateFile(
		srcName,
		windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	// FILE_RENAME_INFO is variable-length: the name follows the fixed header,
	// and FileNameLength counts its bytes without the terminating NUL.
	nameBytes := (len(dstName) - 1) * 2
	var header fileRenameInfo
	buffer := make([]byte, int(unsafe.Offsetof(header.FileName))+nameBytes+2)
	info := (*fileRenameInfo)(unsafe.Pointer(&buffer[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.FileNameLength = uint32(nameBytes)
	copy(unsafe.Slice(&info.FileName[0], len(dstName)), dstName)

	return windows.SetFileInformationByHandle(handle, windows.FileRenameInfoEx, &buffer[0], uint32(len(buffer)))
}
