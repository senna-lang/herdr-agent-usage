/**
 * Tests for CollectAllProviderLimits facade.
 */
package limits

import (
	"os"
	"path/filepath"
	"testing"
)

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
		Claude: []ClaudeProfileCollector{{
			ID: "claude", Label: "Claude",
			Collector: func(c *string, now int64) ProviderLimits {
				return ProviderLimits{ProviderID: "claude", Label: "Claude", Source: "test", FetchedAtMs: now}
			},
		}},
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

func TestCollectAllProviderLimits_MultipleClaudeProfiles(t *testing.T) {
	got := CollectAllProviderLimits(nil, 100, CollectOptions{
		Claude: []ClaudeProfileCollector{
			{ID: "claude", Label: "Claude", Collector: func(_ *string, now int64) ProviderLimits {
				return ProviderLimits{ProviderID: "claude", Label: "Claude", Source: "test-a", FetchedAtMs: now}
			}},
			{ID: "claude-secondary", Label: "Claude (secondary)", Collector: func(_ *string, now int64) ProviderLimits {
				return ProviderLimits{ProviderID: "claude-secondary", Label: "Claude (secondary)", Source: "test-b", FetchedAtMs: now}
			}},
		},
		Only: map[string]bool{"claude": true, "claude-secondary": true, "codex": true, "opencode": true, "grok": true},
	})
	if len(got) != 5 {
		t.Fatalf("len=%d want 5: %+v", len(got), got)
	}
	if got[0].ProviderID != "claude" || got[0].Source != "test-a" {
		t.Fatalf("profile 1 = %+v", got[0])
	}
	if got[1].ProviderID != "claude-secondary" || got[1].Source != "test-b" {
		t.Fatalf("profile 2 = %+v", got[1])
	}
	if got[2].ProviderID != "codex" {
		t.Fatalf("codex should follow all claude profiles, got %+v", got[2])
	}
}

func TestCollectAllProviderLimits_ClaudeProfileFilteredByOnly(t *testing.T) {
	secondaryCalled := false
	got := CollectAllProviderLimits(nil, 100, CollectOptions{
		Claude: []ClaudeProfileCollector{
			{ID: "claude", Label: "Claude", Collector: func(_ *string, now int64) ProviderLimits {
				return ProviderLimits{ProviderID: "claude", Label: "Claude", Source: "test-a", FetchedAtMs: now}
			}},
			{ID: "claude-secondary", Label: "Claude (secondary)", Collector: func(_ *string, now int64) ProviderLimits {
				secondaryCalled = true
				return ProviderLimits{ProviderID: "claude-secondary", Label: "Claude (secondary)", Source: "test-b", FetchedAtMs: now}
			}},
		},
		Only: map[string]bool{"claude": true},
	})
	if len(got) != 1 || got[0].ProviderID != "claude" {
		t.Fatalf("got %+v", got)
	}
	if secondaryCalled {
		t.Fatal("filtered-out profile's collector must not run")
	}
}

func TestDefaultCollectOptions_MultiProfileGroupsEveryProfileUnderClaude(t *testing.T) {
	pluginConfigDir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir)
	dirLabeled := t.TempDir()
	dirUnlabeled := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirLabeled, ".claude.json"),
		[]byte(`{"oauthAccount":{"emailAddress":"primary@example.com"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirUnlabeled, ".claude.json"),
		[]byte(`{"oauthAccount":{"emailAddress":"secondary@example.com"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	toml := "[[claude.profiles]]\n" +
		"id = \"claude\"\n" +
		"label = \"My Work Account\"\n" +
		"config_dir = \"" + dirLabeled + "\"\n\n" +
		"[[claude.profiles]]\n" +
		"id = \"claude-secondary\"\n" +
		"config_dir = \"" + dirUnlabeled + "\"\n"
	if err := os.WriteFile(filepath.Join(pluginConfigDir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := DefaultCollectOptions()
	if len(opts.Claude) != 2 {
		t.Fatalf("want 2 claude collectors, got %d", len(opts.Claude))
	}
	// The explicit label must be preserved as Label; AccountLabel carries the
	// real email so the panel can show it instead once grouped.
	if opts.Claude[0].Label != "My Work Account" {
		t.Fatalf("explicit label must be preserved, got %q", opts.Claude[0].Label)
	}
	got0 := opts.Claude[0].Collector(nil, 0)
	if got0.GroupLabel != "Claude" {
		t.Fatalf("labeled profile should be grouped under Claude, got %q", got0.GroupLabel)
	}
	if got0.AccountLabel != "primary@example.com" {
		t.Fatalf("labeled profile should still resolve AccountLabel from its email, got %q", got0.AccountLabel)
	}
	// The unlabeled profile keeps its id as Label, plus the same grouping.
	if opts.Claude[1].Label != "claude-secondary" {
		t.Fatalf("unlabeled profile label should default to id, got %q", opts.Claude[1].Label)
	}
	got1 := opts.Claude[1].Collector(nil, 0)
	if got1.GroupLabel != "Claude" {
		t.Fatalf("unlabeled profile should be grouped under Claude, got %q", got1.GroupLabel)
	}
	if got1.AccountLabel != "secondary@example.com" {
		t.Fatalf("unlabeled profile should resolve AccountLabel from its email, got %q", got1.AccountLabel)
	}
}

func TestDefaultCollectOptions_SingleProfileNotGrouped(t *testing.T) {
	pluginConfigDir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir)
	// Isolate from the real machine's ~/.claude.json (age/content varies by
	// machine and would otherwise make this test's Note assertion flaky).
	t.Setenv("HOME", t.TempDir())
	// No [[claude.profiles]] configured -> single synthesized default; grouping
	// only kicks in once there are 2+ profiles to disambiguate.
	opts := DefaultCollectOptions()
	if len(opts.Claude) != 1 {
		t.Fatalf("want 1 (default) claude collector, got %d", len(opts.Claude))
	}
	got := opts.Claude[0].Collector(nil, 0)
	if got.GroupLabel != "" {
		t.Fatalf("single-profile mode should not set GroupLabel, got %q", got.GroupLabel)
	}
	if got.AccountLabel != "" {
		t.Fatalf("single-profile mode should not set AccountLabel, got %q", got.AccountLabel)
	}
}
