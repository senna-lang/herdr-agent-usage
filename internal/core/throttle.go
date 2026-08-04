/**
 * Decides whether a sidebar metadata token needs a Herdr CLI write by
 * comparing it against the pane's current server-side tokens, so unchanged
 * settle/focus events do not spawn redundant writes.
 */
package core

// ShouldWriteToken reports whether value differs from the pane's current
// server-side token (absent reads as ""). Force always requests a write.
func ShouldWriteToken(current map[string]string, name, value string, force bool) bool {
	if force {
		return true
	}
	return current[name] != value
}
