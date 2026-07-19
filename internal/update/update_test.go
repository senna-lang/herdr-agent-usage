package update

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

func TestAccountLimitProviderID(t *testing.T) {
	tests := map[string]string{
		"codex":       "codex",
		"GROK":        "grok",
		"agy":         "antigravity",
		"Antigravity": "antigravity",
		"opencode":    "",
	}
	for agent, want := range tests {
		if got := accountLimitProviderID(agent); got != want {
			t.Errorf("agent=%q got=%q want=%q", agent, got, want)
		}
	}
}

func TestAccountLabelRemainingUsesAntigravityFiveHourWindow(t *testing.T) {
	provider := limits.ProviderLimits{
		Primary:   &limits.LimitWindow{UsedPercentage: 0},
		Secondary: &limits.LimitWindow{UsedPercentage: 6},
	}
	got, ok := accountLabelRemaining("antigravity", provider)
	if !ok || got != 100 {
		t.Fatalf("got=%d ok=%v, want 100 true", got, ok)
	}

	got, ok = accountLabelRemaining("codex", provider)
	if !ok || got != 94 {
		t.Fatalf("codex got=%d ok=%v, want 94 true", got, ok)
	}
}
