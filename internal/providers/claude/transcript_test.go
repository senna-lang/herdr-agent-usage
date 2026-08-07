/**
 * Tests for ExtractLatestUsageFromLines.
 */
package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func assistantLine(isSidechain bool, model string, usage map[string]int) string {
	if model == "" {
		model = "claude-sonnet-5"
	}
	if usage == nil {
		usage = map[string]int{
			"input_tokens": 10, "cache_read_input_tokens": 20,
			"cache_creation_input_tokens": 0, "output_tokens": 5,
		}
	}
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "isSidechain": isSidechain,
		"message": map[string]any{"model": model, "usage": usage},
	})
	return string(b)
}

func TestExtractLatestUsageFromLines_LastAssistant(t *testing.T) {
	lines := []string{
		assistantLine(false, "", map[string]int{"input_tokens": 1, "cache_read_input_tokens": 2, "output_tokens": 3}),
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.InputTokens != 1 || got.CacheReadInputTokens != 2 || got.OutputTokens != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsageFromLines_SkipsTrailingMeta(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{"type": "ai-title", "aiTitle": "some title"})
	lines := []string{
		assistantLine(false, "", map[string]int{"input_tokens": 100, "cache_read_input_tokens": 0, "output_tokens": 1}),
		string(meta),
		string(meta),
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.InputTokens != 100 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsageFromLines_SkipsZeroTotal(t *testing.T) {
	lines := []string{
		assistantLine(false, "", map[string]int{"input_tokens": 50, "cache_read_input_tokens": 0, "output_tokens": 1}),
		assistantLine(false, "", map[string]int{"input_tokens": 0, "cache_read_input_tokens": 0, "output_tokens": 0}),
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.InputTokens != 50 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsageFromLines_SkipsSidechain(t *testing.T) {
	lines := []string{
		assistantLine(false, "", map[string]int{"input_tokens": 50, "cache_read_input_tokens": 0, "output_tokens": 1}),
		assistantLine(true, "", map[string]int{"input_tokens": 999, "cache_read_input_tokens": 0, "output_tokens": 1}),
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.InputTokens != 50 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractLatestUsageFromLines_NoValid(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{"type": "ai-title"})
	if got := ExtractLatestUsageFromLines([]string{string(meta), string(meta)}); got != nil {
		t.Fatalf("got %+v", got)
	}
}

func writeSessionFile(t *testing.T, root, projectDir, sessionID string) string {
	t.Helper()
	dir := filepath.Join(root, projectDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveTranscriptPathForSessionIn_FindsInRoot(t *testing.T) {
	root := t.TempDir()
	want := writeSessionFile(t, root, "-home-user-proj", "sess-1")
	got := ResolveTranscriptPathForSessionIn(root, "sess-1")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveTranscriptPathForSessionIn_MissingReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	if got := ResolveTranscriptPathForSessionIn(root, "no-such-session"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestResolveTranscriptPathForSessionIn_EmptySessionID(t *testing.T) {
	root := t.TempDir()
	if got := ResolveTranscriptPathForSessionIn(root, ""); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestResolveProfileForSession_MatchesCorrectRoot(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeSessionFile(t, rootB, "-home-user-b", "sess-b")

	id, ok := ResolveProfileForSession("sess-b", map[string]string{
		"claude":   rootA,
		"claude-m": rootB,
	})
	if !ok || id != "claude-m" {
		t.Fatalf("ok=%v id=%q", ok, id)
	}
}

func TestResolveProfileForSession_NoMatchRefuses(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	id, ok := ResolveProfileForSession("unknown-session", map[string]string{
		"claude":   rootA,
		"claude-m": rootB,
	})
	if ok {
		t.Fatalf("expected no match, got id=%q", id)
	}
}

func TestResolveProfileForSession_EmptySessionID(t *testing.T) {
	if _, ok := ResolveProfileForSession("", map[string]string{"claude": t.TempDir()}); ok {
		t.Fatal("empty session id must not match")
	}
}

func compactBoundaryLine(postTokens int) string {
	b, _ := json.Marshal(map[string]any{
		"type": "system", "subtype": "compact_boundary",
		"compactMetadata": map[string]any{
			"trigger": "manual", "preTokens": 116631, "postTokens": postTokens,
		},
	})
	return string(b)
}

func TestExtractLatestUsageFromLines_CompactBoundarySupersedesOlderUsage(t *testing.T) {
	lines := []string{
		assistantLine(false, "claude-sonnet-5", map[string]int{"input_tokens": 5, "cache_read_input_tokens": 116000, "output_tokens": 9}),
		compactBoundaryLine(13820),
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.InputTokens != 13820 || got.CacheReadInputTokens != 0 || !got.Compacted {
		t.Fatalf("got %+v, want compacted postTokens 13820", got)
	}
	if got.Model != "claude-sonnet-5" {
		t.Fatalf("model=%q, want carried over for window lookup", got.Model)
	}
}

func TestExtractLatestUsageFromLines_UsageAfterCompactWins(t *testing.T) {
	lines := []string{
		assistantLine(false, "", map[string]int{"input_tokens": 5, "cache_read_input_tokens": 116000, "output_tokens": 9}),
		compactBoundaryLine(13820),
		assistantLine(false, "", map[string]int{"input_tokens": 3, "cache_read_input_tokens": 15000, "output_tokens": 7}),
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.CacheReadInputTokens != 15000 || got.Compacted {
		t.Fatalf("got %+v, want the post-compact assistant row", got)
	}
}

func TestExtractLatestUsageFromLines_OnlyNewestBoundaryCounts(t *testing.T) {
	lines := []string{
		assistantLine(false, "", map[string]int{"input_tokens": 5, "cache_read_input_tokens": 116000, "output_tokens": 9}),
		compactBoundaryLine(50000),
		assistantLine(false, "", map[string]int{"input_tokens": 3, "cache_read_input_tokens": 60000, "output_tokens": 7}),
		compactBoundaryLine(13820),
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.InputTokens != 13820 {
		t.Fatalf("got %+v, want newest boundary's postTokens", got)
	}
}

func TestExtractLatestUsageFromLines_BoundaryWithoutOlderUsage(t *testing.T) {
	got := ExtractLatestUsageFromLines([]string{compactBoundaryLine(13820)})
	if got == nil || got.InputTokens != 13820 || got.Model != "" || !got.Compacted {
		t.Fatalf("got %+v, want model-less compacted postTokens", got)
	}
}
