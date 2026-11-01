/**
 * Tests for account-snapshot fan-out onto sidebar $limit tokens.
 */
package update

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

func intp(v int) *int { return &v }

func window(used float64, minutes int) *limits.LimitWindow {
	return &limits.LimitWindow{UsedPercentage: used, WindowMinutes: intp(minutes)}
}

func TestLimitPublishTargets_SubscriptionPaneGetsMatchingProvider(t *testing.T) {
	providers := []limits.ProviderLimits{{
		ProviderID: "claude",
		Primary:    window(28, 300),
	}}
	panes := []LimitPublishPane{{
		PaneID:           "w1:p1",
		Resolved:         true,
		BillingMode:      limits.BillingSubscription,
		LimitsProviderID: "claude",
	}}
	got := LimitPublishTargets(providers, panes, 0, "")
	if len(got) != 1 || got[0].PaneID != "w1:p1" || got[0].LimitToken != "5h 72%" {
		t.Fatalf("got %#v", got)
	}
	if got[0].ContextToken != nil {
		t.Fatal("single-profile must not touch $context")
	}
}

func TestLimitPublishTargets_TwoPanesSameAccount(t *testing.T) {
	providers := []limits.ProviderLimits{{
		ProviderID: "claude",
		Primary:    window(13, 300),
	}}
	panes := []LimitPublishPane{
		{PaneID: "w1:p1", Resolved: true, BillingMode: limits.BillingSubscription, LimitsProviderID: "claude"},
		{PaneID: "w1:p2", Resolved: true, BillingMode: limits.BillingSubscription, LimitsProviderID: "claude"},
	}
	got := LimitPublishTargets(providers, panes, 0, "")
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].LimitToken != "5h 87%" || got[1].LimitToken != "5h 87%" {
		t.Fatalf("got %#v", got)
	}
}

func TestLimitPublishTargets_RoutesEachPaneToOwnProvider(t *testing.T) {
	providers := []limits.ProviderLimits{
		{ProviderID: "claude", Primary: window(28, 300)},
		{ProviderID: "grok", Primary: window(86, 10080)},
	}
	panes := []LimitPublishPane{
		{PaneID: "p-claude", Resolved: true, BillingMode: limits.BillingSubscription, LimitsProviderID: "claude"},
		{PaneID: "p-grok", Resolved: true, BillingMode: limits.BillingSubscription, LimitsProviderID: "grok"},
	}
	got := LimitPublishTargets(providers, panes, 0, "")
	if len(got) != 2 || got[0].LimitToken != "5h 72%" || got[1].LimitToken != "7d 14%" {
		t.Fatalf("got %#v", got)
	}
}

func TestLimitPublishTargets_SkipsMissingProvider(t *testing.T) {
	got := LimitPublishTargets(nil, []LimitPublishPane{{
		PaneID:           "w1:p1",
		Resolved:         true,
		BillingMode:      limits.BillingSubscription,
		LimitsProviderID: "claude",
	}}, 0, "")
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestLimitPublishTargets_SkipsEmptyLimit(t *testing.T) {
	providers := []limits.ProviderLimits{{ProviderID: "claude"}}
	got := LimitPublishTargets(providers, []LimitPublishPane{{
		PaneID:           "w1:p1",
		Resolved:         true,
		BillingMode:      limits.BillingSubscription,
		LimitsProviderID: "claude",
	}}, 0, "")
	if len(got) != 0 {
		t.Fatalf("empty windows must keep last-known-good, got %#v", got)
	}
}

func TestLimitPublishTargets_SkipsPayAsYouGo(t *testing.T) {
	providers := []limits.ProviderLimits{{
		ProviderID: "claude",
		Primary:    window(28, 300),
	}}
	got := LimitPublishTargets(providers, []LimitPublishPane{{
		PaneID:           "w1:p1",
		Resolved:         true,
		BillingMode:      limits.BillingPayAsYouGo,
		LimitsProviderID: "claude",
	}}, 0, "")
	if len(got) != 0 {
		t.Fatalf("PAYG must stay on the event path, got %#v", got)
	}
}

func TestLimitPublishTargets_SkipsUnresolved(t *testing.T) {
	providers := []limits.ProviderLimits{{
		ProviderID: "claude",
		Primary:    window(28, 300),
	}}
	got := LimitPublishTargets(providers, []LimitPublishPane{{
		PaneID:           "w1:p1",
		Resolved:         false,
		BillingMode:      limits.BillingSubscription,
		LimitsProviderID: "claude",
	}}, 0, "")
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestLimitPublishTargets_UnknownBillingFailOpen(t *testing.T) {
	providers := []limits.ProviderLimits{{
		ProviderID: "opencode",
		Primary:    window(20, 300),
	}}
	got := LimitPublishTargets(providers, []LimitPublishPane{{
		PaneID:           "w1:p1",
		Resolved:         true,
		BillingMode:      limits.BillingUnknown,
		LimitsProviderID: "opencode",
	}}, 0, "")
	if len(got) != 1 || got[0].LimitToken != "5h 80%" {
		t.Fatalf("unknown billing must fail open, got %#v", got)
	}
}

func TestLimitPublishTargets_MultiProfileMovesPercentToContext(t *testing.T) {
	providers := []limits.ProviderLimits{{
		ProviderID: "claude",
		Primary:    window(28, 300),
	}}
	got := LimitPublishTargets(providers, []LimitPublishPane{{
		PaneID:           "w1:p1",
		Resolved:         true,
		BillingMode:      limits.BillingSubscription,
		LimitsProviderID: "claude",
		MultiProfile:     true,
		AccountLabel:     "user@example.com",
		Tokens:           map[string]string{"context_meter": "⛁ 14% (136k)", "context": "5h 99% · ⛁ 14% (136k)"},
	}}, 0, "")
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].LimitToken != "user@example.com" {
		t.Fatalf("limit=%q", got[0].LimitToken)
	}
	if got[0].ContextToken == nil || *got[0].ContextToken != "5h 72% · ⛁ 14% (136k)" {
		t.Fatalf("context=%v", got[0].ContextToken)
	}
}

func TestLimitPublishTargets_MultiProfileWithoutAccountLabelKeepsLimitRow(t *testing.T) {
	providers := []limits.ProviderLimits{{
		ProviderID: "claude",
		Primary:    window(28, 300),
	}}
	got := LimitPublishTargets(providers, []LimitPublishPane{{
		PaneID:           "w1:p1",
		Resolved:         true,
		BillingMode:      limits.BillingSubscription,
		LimitsProviderID: "claude",
		MultiProfile:     true,
	}}, 0, "")
	if len(got) != 1 || got[0].LimitToken != "5h 72%" || got[0].ContextToken != nil {
		t.Fatalf("got %#v", got)
	}
}

func TestLimitPublishTargets_UsedPercentInvertsSidebarNumber(t *testing.T) {
	providers := []limits.ProviderLimits{{
		ProviderID: "claude",
		Primary:    window(28, 300),
	}}
	got := LimitPublishTargets(providers, []LimitPublishPane{{
		PaneID:           "w1:p1",
		Resolved:         true,
		BillingMode:      limits.BillingSubscription,
		LimitsProviderID: "claude",
	}}, 0, core.LimitPercentUsed)
	if len(got) != 1 || got[0].LimitToken != "5h 28%" {
		t.Fatalf("got %#v", got)
	}
}

func TestApplyLimitPublishTargets_SkipsUnchangedLimit(t *testing.T) {
	set := map[string]int{}
	writer := metadataTokenWriter{
		set: func(paneID, _, name, _ string) bool {
			set[paneID+"/"+name]++
			return true
		},
		clear: func(_, _, _ string) bool { return true },
	}
	panes := []LimitPublishPane{{
		PaneID: "w1:p1",
		Tokens: map[string]string{"limit": "5h 72%"},
	}}
	applyLimitPublishTargets(writer, panes, []LimitPublishTarget{
		{PaneID: "w1:p1", LimitToken: "5h 72%"},
	})
	if set["w1:p1/limit"] != 0 {
		t.Fatalf("unchanged token was written: %v", set)
	}
}

func TestApplyLimitPublishTargets_WritesChangedLimitAndContext(t *testing.T) {
	set := map[string]string{}
	writer := metadataTokenWriter{
		set: func(paneID, _, name, value string) bool {
			set[paneID+"/"+name] = value
			return true
		},
		clear: func(_, _, _ string) bool { return true },
	}
	ctx := "5h 72% · ⛁ 14% (136k)"
	panes := []LimitPublishPane{{
		PaneID: "w1:p1",
		Tokens: map[string]string{"limit": "old@example.com", "context": "5h 99% · ⛁ 14% (136k)"},
	}}
	applyLimitPublishTargets(writer, panes, []LimitPublishTarget{
		{PaneID: "w1:p1", LimitToken: "user@example.com", ContextToken: &ctx},
	})
	if set["w1:p1/limit"] != "user@example.com" || set["w1:p1/context"] != ctx {
		t.Fatalf("got %v", set)
	}
}

func TestLimitPublishTargets_CarriesBandFamilyForPeriodicRefresh(t *testing.T) {
	providers := []limits.ProviderLimits{{
		ProviderID: "claude",
		Primary:    window(62, 300),
		Secondary:  window(4, 10080),
	}}
	panes := []LimitPublishPane{{
		PaneID:           "w1:p1",
		Resolved:         true,
		BillingMode:      limits.BillingSubscription,
		LimitsProviderID: "claude",
	}}
	got := LimitPublishTargets(providers, panes, 0, "")
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	target := got[0]
	if !target.UpdateBands {
		t.Fatal("periodic publish must update the band family")
	}
	if target.RemainingPercent != 38 {
		t.Fatalf("RemainingPercent = %v, want 38 (tightest window)", target.RemainingPercent)
	}
	if len(target.WindowBands) != 2 || target.WindowBands[0].Tag != "5h" {
		t.Fatalf("WindowBands = %#v", target.WindowBands)
	}
}

func TestApplyLimitPublishTargets_QuotaChangeRefreshesBandsAndClearsOld(t *testing.T) {
	rec := newBandRecorder()
	// Simulate a live pane: the writer mutates the token map so each publish
	// sees the state the previous one left, like herdr's real metadata does.
	tokens := map[string]string{"limit": "5h 72%", "limit_high": "5h 72%"}
	writer := metadataTokenWriter{
		set: func(paneID, _, name, value string) bool {
			rec.set[paneID+"/"+name] = value
			tokens[name] = value
			return true
		},
		clear: func(paneID, _, name string) bool {
			rec.cleared[paneID+"/"+name] = true
			delete(tokens, name)
			return true
		},
	}
	panes := []LimitPublishPane{{
		PaneID:           "w1:p1",
		Resolved:         true,
		BillingMode:      limits.BillingSubscription,
		LimitsProviderID: "claude",
		Tokens:           tokens,
	}}

	// Quota drops 72% -> 38% remaining (mid band).
	targets := LimitPublishTargets([]limits.ProviderLimits{{
		ProviderID: "claude",
		Primary:    window(62, 300),
	}}, panes, 0, "")
	applyLimitPublishTargets(writer, panes, targets)
	if rec.set["w1:p1/limit_mid"] != "5h 38%" {
		t.Fatalf("mid band not refreshed: %v", rec.set)
	}
	if !rec.cleared["w1:p1/limit_high"] {
		t.Fatalf("old high band not cleared: %v", rec.cleared)
	}
}

func TestLimitPublishTargets_MultiProfileConsecutivePublishesKeepOnlyMeter(t *testing.T) {
	defer limits.SetSidebarWindows("shortest")
	limits.SetSidebarWindows("all")

	pane := LimitPublishPane{
		PaneID:           "w1:p1",
		Resolved:         true,
		BillingMode:      limits.BillingSubscription,
		LimitsProviderID: "claude",
		MultiProfile:     true,
		AccountLabel:     "user@example.com",
		Tokens: map[string]string{
			"limit":         "user@example.com",
			"context_meter": "⛁ 14% (136k)",
			"context":       "5h 72% · 7d 30% · ⛁ 14% (136k)",
		},
	}

	first := LimitPublishTargets([]limits.ProviderLimits{{
		ProviderID: "claude",
		Primary:    window(40, 300),
		Secondary:  window(80, 10080),
	}}, []LimitPublishPane{pane}, 0, "")
	if len(first) != 1 || first[0].ContextToken == nil {
		t.Fatalf("got %#v", first)
	}
	got := *first[0].ContextToken
	want := "5h 60% · 7d 20% · ⛁ 14% (136k)"
	if got != want {
		t.Fatalf("tick 1 context = %q, want %q", got, want)
	}

	// Tick 2: the pane now carries tick 1's output. The prefix must be
	// replaced whole — the first " · " is no longer the prefix boundary.
	pane.Tokens["context"] = got
	second := LimitPublishTargets([]limits.ProviderLimits{{
		ProviderID: "claude",
		Primary:    window(50, 300),
		Secondary:  window(90, 10080),
	}}, []LimitPublishPane{pane}, 0, "")
	got2 := *second[0].ContextToken
	want2 := "5h 50% · 7d 10% · ⛁ 14% (136k)"
	if got2 != want2 {
		t.Fatalf("tick 2 context = %q, want %q (stale windows stacked?)", got2, want2)
	}
}

func TestLimitPublishTargets_ConsecutivePublishesWithoutMeter(t *testing.T) {
	tests := []struct {
		mode       string
		initial    string
		wantFirst  string
		wantSecond string
	}{
		{"all", "5h 72% · 7d 30% · ⛁ 14% (136k)", "5h 60% · 7d 20%", "5h 50% · 7d 10%"},
		{"compact", "72% · 30% · ⛁ 14% (136k)", "60% · 20%", "50% · 10%"},
	}
	for _, tt := range tests {
		for _, meterState := range []string{"absent", "empty"} {
			t.Run(tt.mode+"/"+meterState, func(t *testing.T) {
				limits.SetSidebarWindows(tt.mode)
				t.Cleanup(func() { limits.SetSidebarWindows("shortest") })
				tokens := map[string]string{"context": tt.initial}
				if meterState == "empty" {
					tokens["context_meter"] = ""
				}
				pane := LimitPublishPane{
					PaneID:           "w1:p1",
					Resolved:         true,
					BillingMode:      limits.BillingSubscription,
					LimitsProviderID: "claude",
					MultiProfile:     true,
					AccountLabel:     "personal",
					Tokens:           tokens,
				}
				ticks := []struct {
					usedShort float64
					usedWeek  float64
					want      string
				}{
					{40, 80, tt.wantFirst},
					{50, 90, tt.wantSecond},
				}
				for i, tick := range ticks {
					providers := []limits.ProviderLimits{{ProviderID: "claude", Primary: window(tick.usedShort, 300), Secondary: window(tick.usedWeek, 10080)}}
					targets := LimitPublishTargets(providers, []LimitPublishPane{pane}, 0, "")
					if len(targets) != 1 || targets[0].ContextToken == nil {
						t.Fatalf("tick %d: missing context target: %#v", i+1, targets)
					}
					if targets[0].LimitToken != pane.AccountLabel {
						t.Errorf("tick %d: account label = %q", i+1, targets[0].LimitToken)
					}
					got := *targets[0].ContextToken
					if got != tick.want {
						t.Errorf("tick %d: context = %q, want %q", i+1, got, tick.want)
					}
					pane.Tokens["context"] = got
				}
			})
		}
	}
}
