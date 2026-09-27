//go:build windows

package herdrcli

import (
	"os"
	"path/filepath"
	"strings"
)

// defaultPathExt is the Windows default when PATHEXT is unset.
const defaultPathExt = ".COM;.EXE;.BAT;.CMD"

// isRunnableFile reports whether path names an existing regular file that
// Windows can execute. Windows has no executable bit, so executability comes
// from the file extension, matched case-insensitively against PATHEXT exactly
// as the shell does when it launches a command.
func isRunnableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	ext := filepath.Ext(path)
	if ext == "" {
		return false
	}
	pathExt := os.Getenv("PATHEXT")
	if pathExt == "" {
		pathExt = defaultPathExt
	}
	for _, candidate := range strings.Split(pathExt, ";") {
		if strings.EqualFold(strings.TrimSpace(candidate), ext) {
			return true
		}
	}
	return false
}
