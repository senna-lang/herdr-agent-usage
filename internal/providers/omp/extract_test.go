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
