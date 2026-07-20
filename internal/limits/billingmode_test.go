/**
 * Tests for billing-mode detection and the subscription display gate.
 */
package limits

import "testing"

func sp(s string) *string { return &s }

func TestCombineBillingModes(t *testing.T) {
	cases := []struct {
		a, b, want BillingMode
	}{
		{BillingUnknown, BillingUnknown, BillingUnknown},
		{BillingSubscription, BillingUnknown, BillingSubscription},
		{BillingUnknown, BillingSubscription, BillingSubscription},
		{BillingPayAsYouGo, BillingSubscription, BillingPayAsYouGo},
		{BillingSubscription, BillingPayAsYouGo, BillingPayAsYouGo},
		{BillingPayAsYouGo, BillingUnknown, BillingPayAsYouGo},
	}
	for _, c := range cases {
		if got := CombineBillingModes(c.a, c.b); got != c.want {
			t.Fatalf("Combine(%v,%v)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestOpenCodeBillingModeFromProviderID(t *testing.T) {
	if got := OpenCodeBillingModeFromProviderID(sp("opencode-go")); got != BillingSubscription {
		t.Fatalf("opencode-go: got %v", got)
	}
	if got := OpenCodeBillingModeFromProviderID(sp("deepseek")); got != BillingPayAsYouGo {
		t.Fatalf("deepseek: got %v", got)
	}
	if got := OpenCodeBillingModeFromProviderID(sp("ollama")); got != BillingPayAsYouGo {
		t.Fatalf("ollama: got %v", got)
	}
	if got := OpenCodeBillingModeFromProviderID(nil); got != BillingUnknown {
		t.Fatalf("nil: got %v", got)
	}
	if got := OpenCodeBillingModeFromProviderID(sp("")); got != BillingUnknown {
		t.Fatalf("empty: got %v", got)
	}
}

func TestCodexBillingModeFromLines_SubscriptionWithRateLimits(t *testing.T) {
	lines := []string{
		`{"type":"event_msg","payload":{"type":"token_count","info":{"rate_limits":{"primary":{"used_percent":12},"plan_type":"plus"}}}}`,
	}
	if got := CodexBillingModeFromLines(lines); got != BillingSubscription {
		t.Fatalf("got %v want Subscription", got)
	}
}

func TestCodexBillingModeFromLines_APIKeyPlanType(t *testing.T) {
	lines := []string{
		`{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":0},"plan_type":"apikey"}}}`,
	}
	if got := CodexBillingModeFromLines(lines); got != BillingPayAsYouGo {
		t.Fatalf("got %v want PayAsYouGo", got)
	}
}

func TestCodexBillingModeFromLines_TokenCountWithoutRateLimits(t *testing.T) {
	lines := []string{
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"total_tokens":100}}}}`,
	}
	if got := CodexBillingModeFromLines(lines); got != BillingPayAsYouGo {
		t.Fatalf("got %v want PayAsYouGo", got)
	}
}

func TestCodexBillingModeFromLines_NoTokenCount(t *testing.T) {
	lines := []string{
		`{"type":"session_meta","payload":{"id":"x"}}`,
		"",
		"not json",
	}
	if got := CodexBillingModeFromLines(lines); got != BillingUnknown {
		t.Fatalf("got %v want Unknown", got)
	}
}

func TestClaudeBillingModeFromJSON(t *testing.T) {
	sub := `{"cachedUsageUtilization":{"utilization":{"five_hour":{"utilization":40}}}}`
	if got := ClaudeBillingModeFromJSON(sub); got != BillingSubscription {
		t.Fatalf("utilization: got %v", got)
	}
	subType := `{"oauthAccount":{"billingType":"stripe_subscription"}}`
	if got := ClaudeBillingModeFromJSON(subType); got != BillingSubscription {
		t.Fatalf("billingType subscription: got %v", got)
	}
	api := `{"oauthAccount":{"billingType":"prepaid_credits"}}`
	if got := ClaudeBillingModeFromJSON(api); got != BillingPayAsYouGo {
		t.Fatalf("billingType api: got %v", got)
	}
	bare := `{"projects":{}}`
	if got := ClaudeBillingModeFromJSON(bare); got != BillingPayAsYouGo {
		t.Fatalf("no account: got %v", got)
	}
	if got := ClaudeBillingModeFromJSON("not json"); got != BillingUnknown {
		t.Fatalf("bad json: got %v", got)
	}
}

func TestGrokBillingModeFromAuthMode(t *testing.T) {
	if got := GrokBillingModeFromAuthMode(sp("oidc")); got != BillingSubscription {
		t.Fatalf("oidc: got %v", got)
	}
	if got := GrokBillingModeFromAuthMode(sp("api-key")); got != BillingPayAsYouGo {
		t.Fatalf("api-key: got %v", got)
	}
	if got := GrokBillingModeFromAuthMode(nil); got != BillingUnknown {
		t.Fatalf("nil: got %v", got)
	}
}

func depsFor(account map[string]BillingMode, pane map[string]BillingMode) BillingDeps {
	return BillingDeps{
		AccountMode: func(providerID string) BillingMode { return account[providerID] },
		PaneMode: func(providerID string, p OpenPaneSnapshot) BillingMode {
			return pane[p.PaneID]
		},
	}
}

func TestBillingProviderFilter_HidesAllPayAsYouGoPanes(t *testing.T) {
	// One opencode pane on deepseek: opencode must be excluded.
	panes := []OpenPaneSnapshot{{PaneID: "p1", Agent: "opencode"}}
	deps := depsFor(nil, map[string]BillingMode{"p1": BillingPayAsYouGo})
	set := BillingProviderFilter(panes, true, deps)
	if set["opencode"] {
		t.Fatalf("opencode should be excluded: %#v", set)
	}
	if !set["claude"] || !set["codex"] || !set["grok"] {
		t.Fatalf("providers without evidence must stay included: %#v", set)
	}
}

func TestBillingProviderFilter_MixedPanesKeepProvider(t *testing.T) {
	// Go pane + deepseek pane: provider stays visible.
	panes := []OpenPaneSnapshot{
		{PaneID: "go", Agent: "opencode"},
		{PaneID: "ds", Agent: "opencode"},
	}
	deps := depsFor(nil, map[string]BillingMode{
		"go": BillingSubscription,
		"ds": BillingPayAsYouGo,
	})
	set := BillingProviderFilter(panes, true, deps)
	if !set["opencode"] {
		t.Fatalf("opencode should stay included: %#v", set)
	}
}

func TestBillingProviderFilter_AccountPayAsYouGoExcludes(t *testing.T) {
	// API-key Claude account: excluded even with an open claude pane.
	panes := []OpenPaneSnapshot{{PaneID: "c1", Agent: "claude"}}
	deps := depsFor(map[string]BillingMode{"claude": BillingPayAsYouGo}, nil)
	set := BillingProviderFilter(panes, true, deps)
	if set["claude"] {
		t.Fatalf("claude should be excluded: %#v", set)
	}
}

func TestBillingProviderFilter_PaneQueryFailedFailsOpen(t *testing.T) {
	deps := depsFor(map[string]BillingMode{"grok": BillingPayAsYouGo}, nil)
	set := BillingProviderFilter(nil, false, deps)
	if !set["claude"] || !set["codex"] || !set["opencode"] {
		t.Fatalf("pane query failure must fail open per account evidence: %#v", set)
	}
	if set["grok"] {
		t.Fatalf("account-level pay-as-you-go still excludes: %#v", set)
	}
}

func TestPaneBillingMode_CombinesAccountAndSession(t *testing.T) {
	pane := OpenPaneSnapshot{PaneID: "p1", Agent: "opencode"}
	deps := depsFor(
		map[string]BillingMode{"opencode": BillingUnknown},
		map[string]BillingMode{"p1": BillingPayAsYouGo},
	)
	if got := PaneBillingMode("opencode", pane, deps); got != BillingPayAsYouGo {
		t.Fatalf("got %v want PayAsYouGo", got)
	}
}

func TestIntersectFilters(t *testing.T) {
	a := map[string]bool{"claude": true, "opencode": true}
	b := map[string]bool{"opencode": true, "grok": true}
	got := IntersectFilters(a, b)
	if len(got) != 1 || !got["opencode"] {
		t.Fatalf("got %#v", got)
	}
	if r := IntersectFilters(nil, b); len(r) != 2 {
		t.Fatalf("nil a should pass through b: %#v", r)
	}
	if r := IntersectFilters(a, nil); len(r) != 2 {
		t.Fatalf("nil b should pass through a: %#v", r)
	}
}
