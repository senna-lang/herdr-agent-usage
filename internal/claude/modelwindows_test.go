/**
 * Tests for ContextWindowFor.
 */
package claude

import "testing"

func TestNormalizeClaudeModelId_Strips1m(t *testing.T) {
	if got := NormalizeClaudeModelID("claude-opus-4-8[1m]"); got != "claude-opus-4-8" {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeClaudeModelID("claude-sonnet-5[1m]"); got != "claude-sonnet-5" {
		t.Fatalf("got %q", got)
	}
}

func TestContextWindowFor_1MDefaults(t *testing.T) {
	models := []string{
		"claude-sonnet-5",
		"claude-fable-5",
		"claude-opus-4-8",
		"claude-opus-4-7",
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-sonnet-4-5",
		"claude-sonnet-4",
	}
	for _, m := range models {
		got := ContextWindowFor(m)
		if got == nil || *got != 1_000_000 {
			t.Fatalf("%s => %#v, want 1M", m, got)
		}
	}
}

func TestContextWindowFor_Sonnet45DateSuffix(t *testing.T) {
	got := ContextWindowFor("claude-sonnet-4-5-20250929")
	if got == nil || *got != 1_000_000 {
		t.Fatalf("got %#v", got)
	}
}

func TestContextWindowFor_1mSuffix(t *testing.T) {
	for _, m := range []string{"claude-opus-4-8[1m]", "claude-fable-5[1m]", "claude-sonnet-4-5[1m]"} {
		got := ContextWindowFor(m)
		if got == nil || *got != 1_000_000 {
			t.Fatalf("%s => %#v", m, got)
		}
	}
}

func TestContextWindowFor_200k(t *testing.T) {
	cases := []string{
		"claude-haiku-4-5-20251001",
		"claude-3-5-haiku",
		"claude-opus-4-5",
		"claude-opus-4-5-20251101",
		"claude-opus-4-1",
		"claude-opus-4-1-20250805",
	}
	for _, m := range cases {
		got := ContextWindowFor(m)
		if got == nil || *got != 200_000 {
			t.Fatalf("%s => %#v, want 200k", m, got)
		}
	}
}

func TestContextWindowFor_LongestFirstSonnet(t *testing.T) {
	if got := ContextWindowFor("claude-sonnet-4-6"); got == nil || *got != 1_000_000 {
		t.Fatalf("sonnet-4-6 => %#v", got)
	}
	if got := ContextWindowFor("claude-sonnet-4-5-20250929"); got == nil || *got != 1_000_000 {
		t.Fatalf("sonnet-4-5 date => %#v", got)
	}
}

func TestContextWindowFor_LongestFirstOpus(t *testing.T) {
	if got := ContextWindowFor("claude-opus-4-5"); got == nil || *got != 200_000 {
		t.Fatalf("opus-4-5 => %#v", got)
	}
	if got := ContextWindowFor("claude-opus-4-8"); got == nil || *got != 1_000_000 {
		t.Fatalf("opus-4-8 => %#v", got)
	}
}

func TestContextWindowFor_BedrockVertex(t *testing.T) {
	cases := []string{
		"us.anthropic.claude-sonnet-5-20250514-v1:0",
		"us.anthropic.claude-opus-4-8-20250514-v1:0",
		"us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	}
	for _, m := range cases {
		got := ContextWindowFor(m)
		if got == nil || *got != 1_000_000 {
			t.Fatalf("%s => %#v", m, got)
		}
	}
}

func TestContextWindowFor_Unknown(t *testing.T) {
	if got := ContextWindowFor("unknown-model-x"); got != nil {
		t.Fatalf("got %#v", got)
	}
	if got := ContextWindowFor("qwen25c-14b-bench"); got != nil {
		t.Fatalf("got %#v", got)
	}
}
