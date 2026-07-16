/**
 * Tests for plan label normalization.
 */
package planlabels

import "testing"

func TestClaudePlanLabel_Pro(t *testing.T) {
	got := ClaudePlanLabel(strPtr("claude_pro"), nil)
	if got == nil || *got != "Pro" {
		t.Fatalf("got %#v, want Pro", got)
	}
}

func TestClaudePlanLabel_Max5xFromTier(t *testing.T) {
	got := ClaudePlanLabel(nil, strPtr("default_claude_max_5x"))
	if got == nil || *got != "Max 5x" {
		t.Fatalf("got %#v, want Max 5x", got)
	}
}

func TestGrokPlanLabel_Lite(t *testing.T) {
	got := GrokPlanLabel(strPtr("SUBSCRIPTION_TIER_SUPER_GROK_LITE"))
	if got == nil || *got != "Lite" {
		t.Fatalf("got %#v, want Lite", got)
	}
}

func TestGrokPlanLabel_SuperGrok(t *testing.T) {
	got := GrokPlanLabel(strPtr("SUBSCRIPTION_TIER_SUPER_GROK"))
	if got == nil || *got != "SuperGrok" {
		t.Fatalf("got %#v, want SuperGrok", got)
	}
}

func TestCodexPlanLabel_Plus(t *testing.T) {
	got := CodexPlanLabel(strPtr("plus"))
	if got == nil || *got != "Plus" {
		t.Fatalf("got %#v, want Plus", got)
	}
}

func TestOpencodePlanLabel_Go(t *testing.T) {
	got := OpencodePlanLabel(strPtr("go"))
	if got == nil || *got != "Go" {
		t.Fatalf("got %#v, want Go", got)
	}
}
