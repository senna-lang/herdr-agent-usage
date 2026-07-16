/**
 * Tests for UsageFromSignals.
 */
package grok

import "testing"

func f64(v float64) *float64 { return &v }

func TestUsageFromSignals_Full(t *testing.T) {
	got := UsageFromSignals(Signals{
		ContextTokensUsed: f64(169_111), ContextWindowTokens: f64(500_000), ContextWindowUsage: f64(33),
	})
	if got == nil || got.ContextTokens != 169_111 || got.WindowTokens == nil || *got.WindowTokens != 500_000 {
		t.Fatalf("got %+v", got)
	}
}

func TestUsageFromSignals_NoWindow(t *testing.T) {
	got := UsageFromSignals(Signals{ContextTokensUsed: f64(1000)})
	if got == nil || got.ContextTokens != 1000 || got.WindowTokens != nil {
		t.Fatalf("got %+v", got)
	}
	got = UsageFromSignals(Signals{ContextTokensUsed: f64(1000), ContextWindowTokens: f64(0)})
	if got == nil || got.ContextTokens != 1000 || got.WindowTokens != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestUsageFromSignals_NullUsed(t *testing.T) {
	if UsageFromSignals(Signals{}) != nil {
		t.Fatal("expected nil")
	}
	if UsageFromSignals(Signals{ContextTokensUsed: f64(0), ContextWindowTokens: f64(500_000)}) != nil {
		t.Fatal("expected nil")
	}
}

func TestParseSignalsJSON(t *testing.T) {
	if _, ok := ParseSignalsJSON("not-json"); ok {
		t.Fatal("expected fail")
	}
	s, ok := ParseSignalsJSON(`{"contextTokensUsed":10,"contextWindowTokens":100}`)
	if !ok || s.ContextTokensUsed == nil || *s.ContextTokensUsed != 10 {
		t.Fatalf("got %+v ok=%v", s, ok)
	}
}
