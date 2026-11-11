package limits

import "testing"

// The everyday config dir signed into a dedicated profile's account shares
// that account's freshest windows and points at the dedicated row.
func TestShareAccountWindows_JoinsTheSameAccountAcrossDirs(t *testing.T) {
	plan := "Max"
	fresh := &LimitWindow{UsedPercentage: 40}
	old := &LimitWindow{UsedPercentage: 5}
	age := 300
	entries := []ProviderLimits{
		{ProviderID: "claude-1", Family: "claude", ProfileLabel: "claude-1", Account: "a@example.com", Primary: old, FetchedAtMs: 100, StaleMinutes: &age, Source: "claude.json cachedUsageUtilization"},
		{ProviderID: "claude-2", Family: "claude", ProfileLabel: "claude-2", Account: "b@example.com", Primary: old, FetchedAtMs: 100},
		{ProviderID: "claude", Family: "claude", ProfileLabel: "claude", Account: "a@example.com", Primary: fresh, FetchedAtMs: 900, PlanType: &plan, Source: "claude statusLine cache"},
		{ProviderID: "codex", Family: "codex", ProfileLabel: "codex", Account: "a@example.com", Primary: old, FetchedAtMs: 50},
		{ProviderID: "grok", Family: "grok"},
	}

	got := ShareAccountWindows(entries)

	one := got[0]
	if one.Primary != fresh || one.FetchedAtMs != 900 || one.StaleMinutes != nil || one.SameAccountAs != "" {
		t.Fatalf("claude-1 = %+v, want the fresher reading and to stay the account's row", one)
	}
	if one.Source != "claude statusLine cache via claude" || one.PlanType == nil || *one.PlanType != "Max" {
		t.Fatalf("claude-1 source/plan = %q/%v", one.Source, one.PlanType)
	}
	if got[2].SameAccountAs != "claude-1" || got[2].Primary != fresh {
		t.Fatalf("claude = %+v, want it folded into claude-1", got[2])
	}
	// Other accounts, other families and unknown accounts are untouched.
	if got[1].SameAccountAs != "" || got[1].Primary != old {
		t.Fatalf("claude-2 = %+v", got[1])
	}
	if got[3].SameAccountAs != "" || got[3].Primary != old {
		t.Fatalf("codex = %+v, a shared email across families is not one account", got[3])
	}
	if got[4].SameAccountAs != "" {
		t.Fatalf("grok = %+v", got[4])
	}
	// The input slice is not modified.
	if entries[0].Primary != old {
		t.Fatal("input mutated")
	}
}

// A folded entry never toasts: its account's row already does.
func TestCheckProviderPrimaryLimits_SkipsAFoldedAccount(t *testing.T) {
	var titles []string
	CheckProviderPrimaryLimits(
		[]ProviderLimits{notifyTestProvider(func(p *ProviderLimits) { p.SameAccountAs = "codex-main" })},
		ProviderNotifyState{},
		notifyNowMs,
		func(title, _ string) bool { titles = append(titles, title); return true },
		defaultClaudeProfiles,
	)
	if len(titles) != 0 {
		t.Fatalf("titles = %v", titles)
	}
}
