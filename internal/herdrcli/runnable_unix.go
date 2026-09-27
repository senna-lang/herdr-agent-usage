//go:build !windows

package herdrcli

import "os"

// isRunnableFile reports whether path names an existing regular file with an
// executable bit set.
func isRunnableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}
