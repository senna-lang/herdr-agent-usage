/**
 * Tests for ToContextUsage.
 */
package claude

import "testing"

func usage(model string, cacheRead int) TranscriptUsage {
	return TranscriptUsage{
		Model:                model,
		CacheReadInputTokens: cacheRead,
	}
}

func TestToContextUsage_KnownModel(t *testing.T) {
	got := ToContextUsage(usage("claude-sonnet-5", 310_000))
	if got.ContextTokens != 310_000 || got.WindowTokens == nil || *got.WindowTokens != 1_000_000 {
		t.Fatalf("got %+v", got)
	}
}

func TestToContextUsage_UnknownModel(t *testing.T) {
	got := ToContextUsage(usage("unknown-model-x", 5_000))
	if got.ContextTokens != 5_000 || got.WindowTokens != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestToContextUsage_HaikuDateSuffix(t *testing.T) {
	got := ToContextUsage(usage("claude-haiku-4-5-20251001", 40_000))
	if got.ContextTokens != 40_000 || got.WindowTokens == nil || *got.WindowTokens != 200_000 {
		t.Fatalf("got %+v", got)
	}
}

func TestToContextUsage_FableAndOpus(t *testing.T) {
	fable := ToContextUsage(usage("claude-fable-5", 130_000))
	if fable.ContextTokens != 130_000 || fable.WindowTokens == nil || *fable.WindowTokens != 200_000 {
		t.Fatalf("fable %+v", fable)
	}
	fable1m := ToContextUsage(usage("claude-fable-5[1m]", 130_000))
	if fable1m.ContextTokens != 130_000 || fable1m.WindowTokens == nil || *fable1m.WindowTokens != 1_000_000 {
		t.Fatalf("fable[1m] %+v", fable1m)
	}
	opus := ToContextUsage(usage("claude-opus-4-8", 108_000))
	if opus.ContextTokens != 108_000 || opus.WindowTokens == nil || *opus.WindowTokens != 1_000_000 {
		t.Fatalf("opus %+v", opus)
	}
}

func TestToContextUsage_CompactedPassthrough(t *testing.T) {
	u := usage("claude-sonnet-5", 0)
	u.InputTokens = 13_820
	u.Compacted = true
	got := ToContextUsage(u)
	if !got.Compacted || got.ContextTokens != 13_820 {
		t.Fatalf("got %+v, want compacted passthrough", got)
	}
}
