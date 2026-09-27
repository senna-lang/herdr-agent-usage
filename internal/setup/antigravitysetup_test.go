/**
 * Tests for Antigravity statusLine setup guidance.
 */
package setup

import (
	"strings"
	"testing"
)

func TestAntigravitySetupLines_MentionsSlashCommandAndBridge(t *testing.T) {
	joined := strings.Join(antigravitySetupLines("/plugin"), "\n")
	for _, want := range []string{
		"/statusline " + statusLineCommand("/plugin", "run-antigravity-statusline.sh", "antigravity-statusline"),
		"/statusline delete",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestAntigravityStatusLineSnippet(t *testing.T) {
	got := AntigravityStatusLineSnippet("/plugin")
	want := "/statusline " + statusLineCommand("/plugin", "run-antigravity-statusline.sh", "antigravity-statusline")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
