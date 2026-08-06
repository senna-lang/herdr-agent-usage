/**
 * Tests for Claude provider session-kind interpretation.
 */
package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/provider"
)

func TestProvider_AgentID(t *testing.T) {
	if Provider.AgentID() != "claude" {
		t.Fatalf("got %q", Provider.AgentID())
	}
}

func TestProvider_NullCases(t *testing.T) {
	if Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "path", Value: "/tmp/x"},
	}) != nil {
		t.Fatal("expected nil for non-id kind")
	}
	if Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: ""},
	}) != nil {
		t.Fatal("expected nil for empty value")
	}
	if Provider.ResolveUsage(provider.UsageResolveInput{Session: nil}) != nil {
		t.Fatal("expected nil for no session")
	}
	if Provider.ResolveUsage(provider.UsageResolveInput{
		Session: &provider.AgentSession{Kind: "id", Value: "00000000-0000-0000-0000-000000000000"},
	}) != nil {
		t.Fatal("expected nil for unknown UUID")
	}
}

func TestProvider_FallsBackToCwdWhenSessionMissing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_ROOT", root)
	dir := filepath.Join(root, "-work-proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := assistantLine(false, "claude-sonnet-5", map[string]int{"input_tokens": 42, "output_tokens": 1})
	writeTranscript(t, filepath.Join(dir, "s.jsonl"), line, time.Now())

	cwd := "/work/proj"
	usage := Provider.ResolveUsage(provider.UsageResolveInput{Session: nil, Cwd: &cwd})
	if usage == nil || usage.ContextTokens != 42 {
		t.Fatalf("usage=%+v, want context 42 via cwd fallback", usage)
	}
}
