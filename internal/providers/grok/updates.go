/**
 * Fallback context estimate from updates.jsonl when signals.json is missing.
 *
 * Grok streams `_meta.totalTokens` on thought/message/tool chunks. The last
 * value is not the UI window meter (often far below real contextTokensUsed
 * in signals.json). Callers must only use this when signals are absent.
 */
package grok

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
)

// How much of the tail of updates.jsonl we scan. Files can be multi-MB; the
// latest totalTokens always lands near the end.
const updatesTailScanBytes = 512 * 1024

// LatestContextTokensFromUpdates returns the last `_meta.totalTokens` seen in
// updates.jsonl and the file's mtime in ms. total=0 means nothing usable.
func LatestContextTokensFromUpdates(path string) (total int, mtimeMs int64) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return 0, 0
	}
	mtimeMs = st.ModTime().UnixMilli()
	raw, err := readFileTail(path, updatesTailScanBytes)
	if err != nil || len(raw) == 0 {
		return 0, mtimeMs
	}
	// If we truncated mid-line, drop the partial first line.
	if len(raw) == updatesTailScanBytes {
		if i := bytes.IndexByte(raw, '\n'); i >= 0 && i+1 < len(raw) {
			raw = raw[i+1:]
		}
	}
	last := 0
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || !bytes.Contains(line, []byte("totalTokens")) {
			continue
		}
		var envelope struct {
			Params *struct {
				Meta *struct {
					TotalTokens *float64 `json:"totalTokens"`
				} `json:"_meta"`
			} `json:"params"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			continue
		}
		if envelope.Params == nil || envelope.Params.Meta == nil || envelope.Params.Meta.TotalTokens == nil {
			continue
		}
		n := *envelope.Params.Meta.TotalTokens
		if !isFinite(n) || n <= 0 {
			continue
		}
		last = int(n)
	}
	return last, mtimeMs
}

func readFileTail(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	if size <= 0 {
		return nil, nil
	}
	start := size - maxBytes
	if start < 0 {
		start = 0
	}
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(f)
}
