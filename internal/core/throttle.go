/**
 * Persists the most recently reported sidebar metadata token values per pane
 * so unchanged settle/focus events do not spawn redundant Herdr CLI writes.
 */
package core

import (
	"os"
	"path/filepath"
	"regexp"
)

var unsafeTokenStateChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func tokenStateFilePath(paneID, tokenName string) (string, error) {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		return "", os.ErrInvalid
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	safePane := unsafeTokenStateChars.ReplaceAllString(paneID, "_")
	safeToken := unsafeTokenStateChars.ReplaceAllString(tokenName, "_")
	return filepath.Join(dir, "last-token-"+safePane+"-"+safeToken+".txt"), nil
}

func readLastToken(paneID, tokenName string) (string, bool) {
	path, err := tokenStateFilePath(paneID, tokenName)
	if err != nil {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// ShouldWriteToken reports whether value differs from the last successful
// write. Force always requests a write.
func ShouldWriteToken(paneID, tokenName, value string, force bool) bool {
	if force {
		return true
	}
	last, ok := readLastToken(paneID, tokenName)
	return !ok || last != value
}

// MarkTokenWritten records a token value after Herdr accepts the metadata
// report. An empty value records a successful clear.
func MarkTokenWritten(paneID, tokenName, value string) {
	path, err := tokenStateFilePath(paneID, tokenName)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(value), 0o644)
	// v0.1.x stored a single custom-status value per pane. Metadata tokens use
	// independent files, so remove the obsolete predecessor when this pane next
	// reports successfully.
	dir := filepath.Dir(path)
	safePane := unsafeTokenStateChars.ReplaceAllString(paneID, "_")
	_ = os.Remove(filepath.Join(dir, "last-status-"+safePane+".txt"))
}
