/**
 * Tests for sidebar metadata write routing and retry behavior.
 */
package update

import (
	"testing"

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

func TestFormatSidebarProviderWith_OMPPiKeepHarnessName(t *testing.T) {
	backendFor := func(providerID string, pane limits.OpenPaneSnapshot) string {
		return "deepseek"
	}
	if got := formatSidebarProviderWith(backendFor, "omp", "omp", limits.OpenPaneSnapshot{}); got != "omp" {
		t.Fatalf("omp: got %q want omp", got)
	}
	if got := formatSidebarProviderWith(backendFor, "pi", "pi", limits.OpenPaneSnapshot{}); got != "pi" {
		t.Fatalf("pi: got %q want pi", got)
	}
}
