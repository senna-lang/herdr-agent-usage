/**
 * Tests for context-only providers: they render a context row and nothing in
 * the limit row, whatever pane activity happens to be attributed to them.
 */
package update

import (
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

// A provider that owns no quota has no ProviderLimits, and without positive
// pay-as-you-go evidence must not fall back to rendering session burn: burn
// describes a billed backend, which a context-only provider is not.
func TestProviderAndLimitText_ContextOnlyProviderHasEmptyLimit(t *testing.T) {
	cases := []struct {
		name                      string
		totalTokens, totalCostUSD float64
	}{
		{"no attributed activity", 0, 0},
		{"activity attributed to the pane", 425_000, 0.42},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			providerText, limitText := formatSidebarBillingTokens(
				limits.BillingUnknown, "cursor", "cursor", nil, tc.totalTokens, tc.totalCostUSD, 1_000, "")
			if limitText != "" {
				t.Errorf("limitText = %q, want empty", limitText)
			}
			if providerText != "cursor" {
				t.Errorf("providerText = %q, want the provider label", providerText)
			}
		})
	}
}
