/**
 * Tests for ToContextUsage.
 */
package claude

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

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

func TestToContextUsage_FableAndOpus1M(t *testing.T) {
	fable := ToContextUsage(usage("claude-fable-5", 130_000))
	if fable.ContextTokens != 130_000 || fable.WindowTokens == nil || *fable.WindowTokens != 1_000_000 {
		t.Fatalf("fable %+v", fable)
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

func TestToContextUsage_SplitsLatestAndSessionCache(t *testing.T) {
	u := usage("claude-sonnet-5", 180)
	u.InputTokens = 20
	u.CacheCreationInputTokens = 0
	u.SessionCache = core.CacheFromTokenCounts(150, 1250, 100)
	got := ToContextUsage(u)
	if got.Cache == nil || got.Cache.FreshInputTokens != 20 || got.Cache.ReadTokens != 180 {
		t.Fatalf("latest cache %+v", got.Cache)
	}
	if got.SessionCache == nil || got.SessionCache.FreshInputTokens != 150 || got.SessionCache.ReadTokens != 1250 {
		t.Fatalf("session cache %+v", got.SessionCache)
	}
}

func TestToContextUsage_CompactedDropsCache(t *testing.T) {
	u := usage("claude-sonnet-5", 116000)
	u.InputTokens = 13_820
	u.Compacted = true
	u.SessionCache = core.CacheFromTokenCounts(5, 116000, 0)
	got := ToContextUsage(u)
	if !got.Compacted || got.Cache != nil || got.SessionCache != nil {
		t.Fatalf("compacted must drop cache: %+v", got)
	}
}
