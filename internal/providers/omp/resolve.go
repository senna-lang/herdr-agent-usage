/**
 * Reads session usage from an OMP / pi agent_session path.
 */
package omp

import (
	"bufio"
	"os"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/fsutil"
	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

const tailScanBytes = 1024 * 1024

func expandHome(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home + path[1:]
	}
	return path
}

// SessionPathFromInput returns the OMP jsonl path from a herdr agent_session.
// OMP reports kind="path"; value is the absolute (or ~/...) jsonl path.
func SessionPathFromInput(input provider.UsageResolveInput) string {
	if input.Session == nil {
		return ""
	}
	if input.Session.Kind != "path" {
		return ""
	}
	return expandHome(strings.TrimSpace(input.Session.Value))
}

// SessionPathFromSnapshotValue accepts either a path session value or a bare
// path string stored on OpenPaneSnapshot.SessionID by the sidebar updater.
func SessionPathFromSnapshotValue(value string) string {
	value = expandHome(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if strings.HasSuffix(value, ".jsonl") {
		return value
	}
	return ""
}

// ResolveUsageForPath returns ContextUsage for an OMP session jsonl path.
func ResolveUsageForPath(path string) *core.ContextUsage {
	path = expandHome(path)
	if path == "" {
		return nil
	}
	lines, err := fsutil.ReadLastNLines(path, tailScanBytes)
	if err != nil {
		return nil
	}
	usage := ExtractLatestUsageFromLines(lines)
	if usage == nil {
		return nil
	}
	out := core.ContextUsage{ContextTokens: usage.ContextTokens}
	if window := ContextWindowFor(usage.Provider, usage.Model); window != nil {
		out.WindowTokens = window
	}
	return &out
}

// BackendIDForPath returns the latest assistant provider id for the session.
func BackendIDForPath(path string) string {
	path = expandHome(path)
	if path == "" {
		return ""
	}
	lines, err := fsutil.ReadLastNLines(path, tailScanBytes)
	if err != nil {
		return ""
	}
	return ExtractLatestBackendFromLines(lines)
}

// ActivityForPath sums token/cost totals for the session, optionally windowed.
func ActivityForPath(path string, startMs, endMs int64) (tokens float64, costUSD float64) {
	path = expandHome(path)
	if path == "" {
		return 0, 0
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, `"role":"assistant"`) || strings.Contains(line, `"role": "assistant"`) {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		// Fail closed: a truncated scan would undercount silently.
		return 0, 0
	}
	return SumUsageFromLines(lines, startMs, endMs)
}
