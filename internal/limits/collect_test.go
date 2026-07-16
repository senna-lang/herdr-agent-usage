/**
 * Tests for CollectAllProviderLimits facade.
 */
package limits

import "testing"

func TestCollectAllProviderLimits_OrderAndStubs(t *testing.T) {
	got := CollectAllProviderLimits(nil, 100, CollectOptions{})
	if len(got) != 4 {
		t.Fatalf("len=%d", len(got))
	}
	wantIDs := []string{"claude", "codex", "opencode", "grok"}
	for i, id := range wantIDs {
		if got[i].ProviderID != id {
			t.Fatalf("[%d] id=%q want %q", i, got[i].ProviderID, id)
		}
		if got[i].Note == nil || !containsStr(*got[i].Note, "not configured") {
			t.Fatalf("[%d] expected stub note, got %v", i, got[i].Note)
		}
	}
}

func TestCollectAllProviderLimits_WithCollectorsAndAttach(t *testing.T) {
	cwd := "/tmp"
	got := CollectAllProviderLimits(&cwd, 200, CollectOptions{
		Claude: func(c *string, now int64) ProviderLimits {
			return ProviderLimits{ProviderID: "claude", Label: "Claude", Source: "test", FetchedAtMs: now}
		},
		Codex: func(c *string, now int64) ProviderLimits {
			if c == nil || *c != "/tmp" {
				t.Fatal("cwd not passed")
			}
			return ProviderLimits{ProviderID: "codex", Label: "Codex", Source: "test", FetchedAtMs: now}
		},
		Attach: func(providers []ProviderLimits, nowMs int64) []ProviderLimits {
			if nowMs != 200 || len(providers) != 4 {
				t.Fatalf("attach args")
			}
			providers[0].PaneActivity = &ProviderPaneActivity{WindowMinutes: 300, TotalTokens: 1}
			return providers
		},
	})
	if got[0].PaneActivity == nil || got[0].PaneActivity.TotalTokens != 1 {
		t.Fatalf("attach not applied: %+v", got[0])
	}
	if got[1].Source != "test" {
		t.Fatalf("codex=%+v", got[1])
	}
}

func TestCollectAllProviderLimits_OnlyFiltersProviders(t *testing.T) {
	got := CollectAllProviderLimits(nil, 100, CollectOptions{
		Only: map[string]bool{"claude": true, "grok": true},
	})
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if got[0].ProviderID != "claude" || got[1].ProviderID != "grok" {
		t.Fatalf("ids=%q,%q want claude,grok (display order kept)", got[0].ProviderID, got[1].ProviderID)
	}
}

func TestCollectAllProviderLimits_OnlyEmptyHidesAll(t *testing.T) {
	got := CollectAllProviderLimits(nil, 100, CollectOptions{Only: map[string]bool{}})
	if len(got) != 0 {
		t.Fatalf("len=%d, want 0", len(got))
	}
}

func TestCollectAllProviderLimits_OnlySkipsFilteredCollectors(t *testing.T) {
	codexCalled := false
	got := CollectAllProviderLimits(nil, 100, CollectOptions{
		Only: map[string]bool{"claude": true},
		Codex: func(_ *string, now int64) ProviderLimits {
			codexCalled = true
			return ProviderLimits{ProviderID: "codex", Label: "Codex", Source: "test", FetchedAtMs: now}
		},
		Attach: func(providers []ProviderLimits, _ int64) []ProviderLimits {
			if len(providers) != 1 {
				t.Fatalf("attach got %d providers, want 1 (filtered)", len(providers))
			}
			return providers
		},
	})
	if codexCalled {
		t.Fatal("codex collector ran despite being filtered out")
	}
	if len(got) != 1 || got[0].ProviderID != "claude" {
		t.Fatalf("got %+v", got)
	}
}
