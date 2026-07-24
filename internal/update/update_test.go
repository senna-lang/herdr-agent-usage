/**
 * Tests for sidebar metadata write routing and retry behavior.
 */
package update

import (
	"os"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

func stringPtr(s string) *string { return &s }

func TestPaneCwdForUpdate_OMPPiPreferPaneCwdWithoutSession(t *testing.T) {
	paneCwd := "/project"
	foregroundCwd := "/project/.venv/lib"
	for _, agent := range []string{"omp", "pi"} {
		got := paneCwdForUpdate(herdrcli.PaneInfo{
			Agent:         stringPtr(agent),
			Cwd:           &paneCwd,
			ForegroundCwd: &foregroundCwd,
		})
		if got == nil || *got != paneCwd {
			t.Fatalf("%s: got %v want %q", agent, got, paneCwd)
		}
	}
}

func TestPaneCwdForUpdate_OtherAgentsPreferForegroundCwd(t *testing.T) {
	paneCwd := "/project"
	foregroundCwd := "/project/subdir"
	got := paneCwdForUpdate(herdrcli.PaneInfo{
		Agent:         stringPtr("codex"),
		Cwd:           &paneCwd,
		ForegroundCwd: &foregroundCwd,
	})
	if got == nil || *got != foregroundCwd {
		t.Fatalf("got %v want %q", got, foregroundCwd)
	}
}

func TestWriteMetadataTokenWith_SetSuccessDeduplicates(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	setCalls := 0
	clearCalls := 0
	writer := metadataTokenWriter{
		set: func(_, _, _, _ string) bool {
			setCalls++
			return true
		},
		clear: func(_, _, _ string) bool {
			clearCalls++
			return true
		},
	}

	writeMetadataTokenWith(writer, "w1:p1", "limit", "5h 72%", false)
	writeMetadataTokenWith(writer, "w1:p1", "limit", "5h 72%", false)
	if setCalls != 1 || clearCalls != 0 {
		t.Fatalf("set=%d clear=%d", setCalls, clearCalls)
	}
}

func TestWriteMetadataTokenWith_ClearSuccessDeduplicates(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	setCalls := 0
	clearCalls := 0
	writer := metadataTokenWriter{
		set: func(_, _, _, _ string) bool {
			setCalls++
			return true
		},
		clear: func(_, _, _ string) bool {
			clearCalls++
			return true
		},
	}

	writeMetadataTokenWith(writer, "w1:p1", "context", "", false)
	writeMetadataTokenWith(writer, "w1:p1", "context", "", false)
	if setCalls != 0 || clearCalls != 1 {
		t.Fatalf("set=%d clear=%d", setCalls, clearCalls)
	}
}

func TestWriteMetadataTokenWith_FailureRetries(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	setCalls := 0
	writer := metadataTokenWriter{
		set: func(_, _, _, _ string) bool {
			setCalls++
			return false
		},
		clear: func(_, _, _ string) bool { return false },
	}

	writeMetadataTokenWith(writer, "w1:p1", "limit", "7d 42%", false)
	writeMetadataTokenWith(writer, "w1:p1", "limit", "7d 42%", false)
	if setCalls != 2 {
		t.Fatalf("set=%d want 2 retries", setCalls)
	}
}

func TestFormatSidebarProviderWith_PayAsYouGoNamesBackendOnly(t *testing.T) {
	backendFor := func(providerID string, pane limits.OpenPaneSnapshot) string {
		return "deepseek"
	}
	// The backend replaces the harness rather than appending to it: the
	// sidebar is too narrow to carry both.
	got := formatSidebarProviderWith(backendFor, "opencode", "opencode", limits.OpenPaneSnapshot{})
	if got != "deepseek" {
		t.Fatalf("got %q want %q", got, "deepseek")
	}
}

func TestFormatSidebarProviderWith_SubscriptionKeepsHarnessOnly(t *testing.T) {
	// Subscription panes, non-OpenCode panes, and unresolvable sessions all
	// fall back to the bare harness name.
	backendFor := func(providerID string, pane limits.OpenPaneSnapshot) string {
		return ""
	}
	if got := formatSidebarProviderWith(backendFor, "claude", "claude", limits.OpenPaneSnapshot{}); got != "claude" {
		t.Fatalf("got %q want %q", got, "claude")
	}
}

func TestFormatSidebarProviderWith_EmptyAgentRendersNothing(t *testing.T) {
	backendFor := func(providerID string, pane limits.OpenPaneSnapshot) string {
		return "deepseek"
	}
	if got := formatSidebarProviderWith(backendFor, "", "opencode", limits.OpenPaneSnapshot{}); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestFormatSidebarProviderWith_OMPPiNameCurrentBackend(t *testing.T) {
	backendFor := func(providerID string, pane limits.OpenPaneSnapshot) string {
		return "deepseek"
	}
	if got := formatSidebarProviderWith(backendFor, "omp", "omp", limits.OpenPaneSnapshot{}); got != "deepseek" {
		t.Fatalf("omp: got %q want deepseek", got)
	}
	if got := formatSidebarProviderWith(backendFor, "pi", "pi", limits.OpenPaneSnapshot{}); got != "deepseek" {
		t.Fatalf("pi: got %q want deepseek", got)
	}
}

func TestFormatSidebarProviderWith_OMPPiWithoutBackendRendersNothing(t *testing.T) {
	backendFor := func(providerID string, pane limits.OpenPaneSnapshot) string { return "" }
	if got := formatSidebarProviderWith(backendFor, "omp", "omp", limits.OpenPaneSnapshot{}); got != "" {
		t.Fatalf("omp: got %q want empty", got)
	}
	if got := formatSidebarProviderWith(backendFor, "pi", "pi", limits.OpenPaneSnapshot{}); got != "" {
		t.Fatalf("pi: got %q want empty", got)
	}
}

func TestSidebarSecondRowContract(t *testing.T) {
	five, seven := 300, 10080
	tests := []struct {
		name                  string
		mode                  limits.BillingMode
		fallback, display     string
		providerLimits        *limits.ProviderLimits
		tokens, cost          float64
		wantProvider, wantLim string
	}{
		{
			name: "Claude subscription uses provider and shortest numeric window",
			mode: limits.BillingSubscription, fallback: "claude", display: "claude",
			providerLimits: &limits.ProviderLimits{
				Primary:   &limits.LimitWindow{UsedPercentage: 66, WindowMinutes: &seven},
				Secondary: &limits.LimitWindow{UsedPercentage: 13, WindowMinutes: &five},
			},
			wantProvider: "claude", wantLim: "5h 87%",
		},
		{
			name: "routed harness subscription names billing provider",
			mode: limits.BillingSubscription, fallback: "omp", display: "opencode-go",
			providerLimits: &limits.ProviderLimits{
				Primary: &limits.LimitWindow{UsedPercentage: 0, WindowMinutes: &five},
			},
			wantProvider: "opencode-go", wantLim: "5h 100%",
		},
		{
			name: "Codex subscription keeps provider supplied seven day minimum",
			mode: limits.BillingSubscription, fallback: "codex", display: "codex",
			providerLimits: &limits.ProviderLimits{
				Primary: &limits.LimitWindow{UsedPercentage: 67, WindowMinutes: &seven},
			},
			wantProvider: "codex", wantLim: "7d 33%",
		},
		{
			name: "Grok subscription uses its seven day provider window",
			mode: limits.BillingSubscription, fallback: "omp", display: "grok",
			providerLimits: &limits.ProviderLimits{
				Primary: &limits.LimitWindow{UsedPercentage: 86, WindowMinutes: &seven},
			},
			wantProvider: "grok", wantLim: "7d 14%",
		},
		{
			name: "unknown billing preserves fail open subscription display",
			mode: limits.BillingUnknown, fallback: "opencode", display: "",
			providerLimits: &limits.ProviderLimits{
				Primary: &limits.LimitWindow{UsedPercentage: 20, WindowMinutes: &five},
			},
			wantProvider: "opencode", wantLim: "5h 80%",
		},
		{
			name: "token only API burn omits dollars",
			mode: limits.BillingPayAsYouGo, fallback: "deepseek", display: "",
			tokens:       425_000,
			wantProvider: "deepseek", wantLim: "Σ 425k",
		},
		{
			name: "cost capable harness includes dollars",
			mode: limits.BillingPayAsYouGo, fallback: "deepseek", display: "",
			tokens: 425_000, cost: 0.04,
			wantProvider: "deepseek", wantLim: "Σ 425k $0.04",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providerText, limitText := formatSidebarBillingTokens(
				tt.mode, tt.fallback, tt.display, tt.providerLimits,
				tt.tokens, tt.cost, 1_800_000_000_000,
			)
			if providerText != tt.wantProvider || limitText != tt.wantLim {
				t.Fatalf("got provider=%q limit=%q, want provider=%q limit=%q", providerText, limitText, tt.wantProvider, tt.wantLim)
			}
		})
	}
}

func TestResolveSidebarAccountLabel_PrefersEmailOverLabel(t *testing.T) {
	dir := t.TempDir()
	jsonPath := dir + "/.claude.json"
	if err := os.WriteFile(jsonPath, []byte(`{"oauthAccount":{"emailAddress":"you@example.com"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := claude.ClaudeProfile{ID: "claude", Label: "My Work Account", JSONPath: jsonPath}
	if got := resolveSidebarAccountLabel(profile); got != "you@example.com" {
		t.Fatalf("got %q want email", got)
	}
}

func TestResolveSidebarAccountLabel_FallsBackToLabelWithoutEmail(t *testing.T) {
	profile := claude.ClaudeProfile{ID: "claude-secondary", Label: "claude-secondary", JSONPath: t.TempDir() + "/missing.json"}
	if got := resolveSidebarAccountLabel(profile); got != "claude-secondary" {
		t.Fatalf("got %q want label fallback", got)
	}
}

func TestCombineLimitAndContext(t *testing.T) {
	cases := []struct{ limitText, statusText, want string }{
		{"5h 88%", "⛁ 14% (136k)", "5h 88% · ⛁ 14% (136k)"},
		{"", "⛁ 14% (136k)", "⛁ 14% (136k)"},
		{"5h 88%", "", "5h 88%"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := combineLimitAndContext(c.limitText, c.statusText); got != c.want {
			t.Fatalf("combineLimitAndContext(%q, %q) = %q, want %q", c.limitText, c.statusText, got, c.want)
		}
	}
}

func TestReserveColumnsFor(t *testing.T) {
	base := 20
	got := reserveColumnsFor(&base, "you@example.com")
	if got == nil {
		t.Fatal("want non-nil budget")
	}
	// 20 - (15 for the email + 3 for " · ") = 2, floored to 3.
	if *got != 3 {
		t.Fatalf("got %d want 3 (floored)", *got)
	}

	wide := 60
	got = reserveColumnsFor(&wide, "you@example.com")
	if *got != 60-len("you@example.com")-3 {
		t.Fatalf("got %d want %d", *got, 60-len("you@example.com")-3)
	}

	if got := reserveColumnsFor(nil, "you@example.com"); got != nil {
		t.Fatal("nil budget must stay nil")
	}
	if got := reserveColumnsFor(&wide, ""); *got != wide {
		t.Fatal("empty prefix must not shrink the budget")
	}
}
