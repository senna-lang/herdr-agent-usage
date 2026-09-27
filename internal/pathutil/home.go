package pathutil

import (
	"path/filepath"
	"strings"
)

// ExpandHome expands a leading "~" in a user-configured path against home.
// Nothing else expands it when the value comes from TOML or an env var, so a
// literal "~/.claude" would otherwise name a directory called "~". "~/" is
// accepted on every platform; on Windows the native "~\" is accepted too. An
// empty home, a "~user" form, or a path without a leading "~" is returned
// unchanged, and the result is not cleaned: callers own normalization.
func ExpandHome(path, home string) string {
	if home == "" {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") ||
		(filepath.Separator == '\\' && strings.HasPrefix(path, `~\`)) {
		return filepath.Join(home, path[1:])
	}
	return path
}
