package omp

import "testing"

func TestExtractLatestUsageFromLines(t *testing.T) {
	lines := []string{
		`{"type":"message","message":{"role":"user","content":[]}}`,
		`{"type":"message","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","usage":{"totalTokens":100,"cost":{"total":0.01}},"contextSnapshot":{"promptTokens":250}}}`,
		`{"type":"message","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","usage":{"totalTokens":200,"cost":{"total":0.02}},"contextSnapshot":{"promptTokens":400}}}`,
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil {
		t.Fatal("expected usage")
	}
	if got.ContextTokens != 400 || got.TotalTokens != 200 || got.Provider != "deepseek" {
		t.Fatalf("got %#v", got)
	}
}

func TestSumUsageFromLines(t *testing.T) {
	lines := []string{
		`{"type":"message","message":{"role":"assistant","timestamp":100,"usage":{"totalTokens":10,"cost":{"total":0.1}}}}`,
		`{"type":"message","message":{"role":"assistant","timestamp":200,"usage":{"totalTokens":20,"cost":{"total":0.2}}}}`,
		`{"type":"message","message":{"role":"assistant","timestamp":300,"usage":{"totalTokens":40,"cost":{"total":0.4}}}}`,
	}
	tokens, cost := SumUsageFromLines(lines, 0, 0)
	if tokens != 70 || cost < 0.699 || cost > 0.701 {
		t.Fatalf("all: tokens=%v cost=%v", tokens, cost)
	}
	tokens, cost = SumUsageFromLines(lines, 150, 250)
	if tokens != 20 || cost < 0.199 || cost > 0.201 {
		t.Fatalf("window: tokens=%v cost=%v", tokens, cost)
	}
}

func TestSumUsageForProviderFromLines_FiltersBackend(t *testing.T) {
	lines := []string{
		`{"type":"message","message":{"role":"assistant","provider":"deepseek","timestamp":100,"usage":{"totalTokens":10,"cost":{"total":0.1}}}}`,
		`{"type":"message","message":{"role":"assistant","provider":"openai","timestamp":200,"usage":{"totalTokens":20,"cost":{"total":0.2}}}}`,
		`{"type":"message","message":{"role":"assistant","provider":"deepseek","timestamp":300,"usage":{"totalTokens":40,"cost":{"total":0.4}}}}`,
	}
	tokens, cost := SumUsageForProviderFromLines(lines, "deepseek", 0, 0)
	if tokens != 50 || cost < 0.499 || cost > 0.501 {
		t.Fatalf("deepseek: tokens=%v cost=%v", tokens, cost)
	}
	tokens, cost = SumUsageForProviderFromLines(lines, "openai", 0, 0)
	if tokens != 20 || cost < 0.199 || cost > 0.201 {
		t.Fatalf("openai: tokens=%v cost=%v", tokens, cost)
	}
}

func TestExtractLatestBackendFromLines(t *testing.T) {
	lines := []string{
		`{"type":"message","message":{"role":"assistant","provider":"deepseek","model":"x","usage":{"totalTokens":1},"contextSnapshot":{"promptTokens":1}}}`,
		`{"type":"message","message":{"role":"assistant","provider":"openai","model":"y","usage":{"totalTokens":1},"contextSnapshot":{"promptTokens":1}}}`,
	}
	if got := ExtractLatestBackendFromLines(lines); got != "openai" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractLatestUsageFromStockPi(t *testing.T) {
	lines := []string{
		`{"type":"message","message":{"role":"assistant","provider":"openai-codex","model":"gpt-5.6-sol","usage":{"input":100,"output":20,"cacheRead":400,"cacheWrite":0,"totalTokens":520},"stopReason":"stop"}}`,
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.ContextTokens != 520 || got.TotalTokens != 520 || got.Model != "gpt-5.6-sol" {
		t.Fatalf("got %#v", got)
	}
}

func TestExtractLatestUsageFallsBackToUsageComponents(t *testing.T) {
	lines := []string{
		`{"type":"message","message":{"role":"assistant","provider":"anthropic","model":"claude-sonnet-5","usage":{"input":100,"output":20,"cacheRead":400,"cacheWrite":10},"stopReason":"stop"}}`,
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.ContextTokens != 530 || got.TotalTokens != 530 {
		t.Fatalf("got %#v", got)
	}
}

func TestExtractLatestUsageSkipsFailedAssistant(t *testing.T) {
	lines := []string{
		`{"type":"message","message":{"role":"assistant","provider":"anthropic","model":"good","usage":{"totalTokens":100},"stopReason":"stop"}}`,
		`{"type":"message","message":{"role":"assistant","provider":"anthropic","model":"failed","usage":{"totalTokens":999},"stopReason":"error"}}`,
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.ContextTokens != 100 || got.Model != "good" {
		t.Fatalf("got %#v", got)
	}
}

func TestExtractLatestUsageUnknownImmediatelyAfterCompaction(t *testing.T) {
	lines := []string{
		`{"type":"message","id":"a1","parentId":null,"message":{"role":"assistant","usage":{"totalTokens":90000},"stopReason":"stop"}}`,
		`{"type":"compaction","id":"cmp1","parentId":"a1","tokensBefore":90000,"summary":"summary"}`,
	}
	if got := ExtractLatestUsageFromLines(lines); got != nil {
		t.Fatalf("got %#v", got)
	}

	lines = append(lines, `{"type":"message","id":"a2","parentId":"cmp1","message":{"role":"assistant","usage":{"totalTokens":22000},"stopReason":"stop"}}`)
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.ContextTokens != 22000 {
		t.Fatalf("post-compaction got %#v", got)
	}
}

func TestExtractLatestUsageFollowsActiveBranch(t *testing.T) {
	lines := []string{
		`{"type":"message","id":"root","parentId":null,"message":{"role":"assistant","model":"root-model","usage":{"totalTokens":100},"stopReason":"stop"}}`,
		`{"type":"message","id":"old","parentId":"root","message":{"role":"assistant","model":"abandoned-model","usage":{"totalTokens":999},"stopReason":"stop"}}`,
		`{"type":"message","id":"new-user","parentId":"root","message":{"role":"user","content":"new branch"}}`,
	}
	got := ExtractLatestUsageFromLines(lines)
	if got == nil || got.ContextTokens != 100 || got.Model != "root-model" {
		t.Fatalf("got %#v", got)
	}
}
