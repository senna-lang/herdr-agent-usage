/**
 * Persists the most recent display text per pane in HERDR_PLUGIN_STATE_DIR so
 * we can skip writes to herdr when usage hasn't changed.
 *
 * We intentionally avoid time-based throttling: it would drop the tail event
 * during a burst. We always compute the latest usage and no-op when the
 * rendered string is identical.
 */
package core

import (
	"os"
	"path/filepath"
	"regexp"
)

const clearedSentinel = ""

var unsafePaneChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func stateDir() (string, error) {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		return "", os.ErrInvalid
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func stateFilePath(paneID string) (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	safe := unsafePaneChars.ReplaceAllString(paneID, "_")
	return filepath.Join(dir, "last-status-"+safe+".txt"), nil
}

func readLastStatus(paneID string) (string, bool) {
	path, err := stateFilePath(paneID)
	if err != nil {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func writeLastStatus(paneID, statusText string) {
	path, err := stateFilePath(paneID)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(statusText), 0o644)
}

// ShouldWriteStatus returns true only when the text differs from the last written display.
// When force is true, always returns true.
func ShouldWriteStatus(paneID, statusText string, force bool) bool {
	if force {
		return true
	}
	last, ok := readLastStatus(paneID)
	if !ok {
		return true
	}
	return last != statusText
}

// MarkStatusWritten records the last written display text.
func MarkStatusWritten(paneID, statusText string) {
	writeLastStatus(paneID, statusText)
}

// MarkStatusCleared records the cleared sentinel.
func MarkStatusCleared(paneID string) {
	writeLastStatus(paneID, clearedSentinel)
}

// IsAlreadyCleared reports whether the persisted state is already the cleared sentinel.
func IsAlreadyCleared(paneID string) bool {
	last, ok := readLastStatus(paneID)
	return ok && last == clearedSentinel
}
