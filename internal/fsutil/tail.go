/**
 * Small filesystem helpers for reading jsonl session tails and first lines.
 */
package fsutil

import (
	"io"
	"os"
)

// ReadLastNLines reads up to maxBytes from the end of path and splits on newlines.
func ReadLastNLines(path string, maxBytes int) ([]string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	size := st.Size()
	start := size - int64(maxBytes)
	if start < 0 {
		start = 0
	}
	length := size - start
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, length)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil, err
	}
	return splitLines(string(buf)), nil
}

// ReadFirstLine reads until the first newline (or maxBytes).
// session_meta can run tens to hundreds of KB, so a small fixed window would truncate JSON.
func ReadFirstLine(path string, maxBytes int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var chunks []byte
	buf := make([]byte, 64*1024)
	var offset int64
	for offset < int64(maxBytes) {
		toRead := int64(len(buf))
		if int64(maxBytes)-offset < toRead {
			toRead = int64(maxBytes) - offset
		}
		n, err := f.ReadAt(buf[:toRead], offset)
		if n > 0 {
			chunks = append(chunks, buf[:n]...)
			offset += int64(n)
			if i := indexByte(chunks, '\n'); i >= 0 {
				return string(chunks[:i]), nil
			}
		}
		if err == io.EOF || n == 0 {
			break
		}
		if err != nil {
			return "", err
		}
	}
	text := trimSpace(string(chunks))
	if text == "" {
		return "", os.ErrNotExist
	}
	return text, nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start <= len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}
