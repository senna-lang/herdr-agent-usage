/**
 * Portable absolute paths for test fixtures.
 *
 * Profile and config tests describe directories with readable Unix literals
 * such as "/home/u/.claude". On Windows those are not absolute (no volume), so
 * the resolvers correctly reject them as relative and the fixtures stop testing
 * what they describe. Abs keeps the literals readable while producing a path
 * that is absolute on the platform running the test. Only tests import this
 * package.
 */
package testpath

import (
	"os"
	"path/filepath"
)

// Abs converts a slash-separated absolute fixture path to an absolute path on
// the current platform. On Unix it returns p unchanged. On Windows it prefixes
// the volume of the temp directory and converts separators, preserving
// trailing separators and "." segments so path-normalization tests still see
// the spelling they are written for: "/home/u/.claude/" becomes
// `C:\home\u\.claude\`.
func Abs(p string) string {
	if filepath.Separator == '/' {
		return p
	}
	return filepath.VolumeName(os.TempDir()) + filepath.FromSlash(p)
}
