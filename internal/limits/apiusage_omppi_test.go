package limits

import "testing"

func TestOMPPiUsageRowsByBackendFromLines(t *testing.T) {
	lines := []string{
		`{"type":"message","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","timestamp":1000,"usage":{"totalTokens":100,"cost":{"total":0.01}}}}`,
		`{"type":"message","message":{"role":"assistant","provider":"cursor","model":"grok","timestamp":2000,"usage":{"totalTokens":50,"cost":{"total":0}}}}`,
		`{"type":"message","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","timestamp":0,"usage":{"totalTokens":999,"cost":{"total":1}}}}`,
		`{"type":"message","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","usage":{"totalTokens":999,"cost":{"total":1}}}}`,
		`{"type":"message","message":{"role":"user","content":"hi"}}`,
	}
	got := OMPPiUsageRowsByBackendFromLines(lines)
	if len(got["deepseek"]) != 1 || got["deepseek"][0].Tokens != 100 || got["deepseek"][0].CostUSD != 0.01 || got["deepseek"][0].CreatedMs != 1000 {
		t.Fatalf("deepseek=%#v", got["deepseek"])
	}
	if len(got["cursor"]) != 1 || got["cursor"][0].Tokens != 50 {
		t.Fatalf("cursor=%#v", got["cursor"])
	}
	if _, ok := got[""]; ok {
		t.Fatalf("unexpected empty backend: %#v", got)
	}
}
