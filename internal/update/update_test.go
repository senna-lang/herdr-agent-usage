/**
 * Tests for sidebar metadata write routing and retry behavior.
 */
package update

import (
	"os"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

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
