package main

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

func TestFlagValuesCollectsRepeatedProviderExclusions(t *testing.T) {
	got := flagValues([]string{
		"--all",
		"--exclude-provider", "Claude",
		"--exclude-provider", "zai",
	}, "--exclude-provider")
	if len(got) != 2 || !got["claude"] || !got["zai"] {
		t.Fatalf("got=%v", got)
	}
}

func TestExcludeProviders(t *testing.T) {
	providers := []limits.ProviderLimits{
		{ProviderID: "claude"},
		{ProviderID: "codex"},
		{ProviderID: "zai"},
	}
	got := excludeProviders(providers, map[string]bool{"claude": true})
	if len(got) != 2 || got[0].ProviderID != "codex" || got[1].ProviderID != "zai" {
		t.Fatalf("got=%v", got)
	}
}
