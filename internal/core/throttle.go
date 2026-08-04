/**
 * Persists the most recently reported sidebar metadata token values per pane
 * so unchanged settle/focus events do not spawn redundant Herdr CLI writes.
 *
 * Each value is stamped with the server generation it was reported to. Herdr
 * keeps metadata tokens in server memory, so a value accepted by an earlier
 * server no longer exists after a restart and must not suppress a re-report.
 */
package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var unsafeTokenStateChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// serverEpochPrefix heads every state file; the rest of the first line is the
// server generation the recorded value was reported to.
const serverEpochPrefix = "#server:"

// serverEpoch identifies the running server generation. The server recreates
// its socket on startup, so the socket's mtime distinguishes restarts without
// spawning a CLI call per token check. Unknown identity ("") still dedupes by
// value alone, which is the pre-epoch behavior.
func serverEpoch() string {
	sock := os.Getenv("HERDR_SOCKET_PATH")
	if sock == "" {
		return ""
	}
	info, err := os.Stat(sock)
	if err != nil {
		return ""
	}
	return strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

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
	// A value recorded against another server generation (or by a pre-epoch
	// version of this file format) refers to tokens that no longer exist.
	header, value, found := strings.Cut(string(b), "\n")
	if !found || !strings.HasPrefix(header, serverEpochPrefix) {
		return "", false
	}
	if strings.TrimPrefix(header, serverEpochPrefix) != serverEpoch() {
		return "", false
	}
	return value, true
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
	_ = os.WriteFile(path, []byte(serverEpochPrefix+serverEpoch()+"\n"+value), 0o644)
	// v0.1.x stored a single custom-status value per pane. Metadata tokens use
	// independent files, so remove the obsolete predecessor when this pane next
	// reports successfully.
	dir := filepath.Dir(path)
	safePane := unsafeTokenStateChars.ReplaceAllString(paneID, "_")
	_ = os.Remove(filepath.Join(dir, "last-status-"+safePane+".txt"))
}
