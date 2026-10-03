/**
 * Tests for Kilo's pay-as-you-go block in the panel.
 *
 * The gap these cover: Kilo panes are classified and labelled by backend like
 * every other harness, but nothing collected their spend, so a Kilo pane on an
 * API key produced no Agent Usage block at all — the pane was on screen with a
 * backend name and nowhere for its spend to appear.
 */
package limits

import (
	"testing"
)

func TestCollectAPIProviderUsage_KiloPaneProducesItsBackendBlock(t *testing.T) {
	// Only this pane is open. The block therefore has to come from Kilo's own
	// store rather than from another harness happening to be installed.
	pane := seedKiloPaneSpend(t, "deepseek", []string{kiloTurn("deepseek", 27, 3, 0.5)}, nil,
		`{"kilo":{"type":"oauth","access":"tok"},"deepseek":{"type":"api","key":"dk"}}`)

	blocks := CollectAPIProviderUsage([]OpenPaneSnapshot{pane}, paneClock())
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want just this pane's backend: %+v", len(blocks), blocks)
	}
	block := blocks[0]
	if block.BackendID != "deepseek" {
		t.Fatalf("backend = %q", block.BackendID)
	}
	if block.Label != backendDisplayLabel("deepseek") {
		t.Fatalf("label = %q, want the shared backend label", block.Label)
	}
	if !block.HasCost {
		t.Fatal("Kilo recorded a cost and the block dropped the cost column")
	}
	if len(block.Windows) != len(APIUsageWindowMinutes) {
		t.Fatalf("windows = %+v", block.Windows)
	}
	day := block.Windows[0]
	if day.WindowMinutes != APIUsageWindowMinutes[0] {
		t.Fatalf("first window = %d minutes", day.WindowMinutes)
	}
	if day.Tokens != 30 || day.CostUSD != 0.5 {
		t.Fatalf("24h spend = %v/%v, want 30/0.50", day.Tokens, day.CostUSD)
	}
	if len(block.Models) != 1 || block.Models[0].ModelID != "some/model" {
		t.Fatalf("models = %+v", block.Models)
	}
	// The pane's own share of that backend, so the block reads like every other.
	if block.PaneActivity == nil || len(block.PaneActivity.Panes) != 1 {
		t.Fatalf("pane activity = %+v", block.PaneActivity)
	}
	if block.PaneActivity.Panes[0].PaneID != pane.PaneID {
		t.Fatalf("share = %+v", block.PaneActivity.Panes[0])
	}
}

func TestCollectAPIProviderUsage_KiloSpendIsScopedToThePanesOwnBackend(t *testing.T) {
	// A block is labelled with the backend its pane is on now, so the gateway spend
	// a session accumulated earlier belongs to the Kilo Pass window and must not be
	// re-presented here. That session really did spend both, so the rule has to be
	// the scope rather than a suppression.
	pane := seedKiloPaneSpend(t, "openrouter", []string{
		kiloTurn("kilo", 9000, 100, 1.25),
		kiloTurn("openrouter", 40, 2, 0.75),
	}, nil, `{"kilo":{"type":"oauth","access":"tok"},"openrouter":{"type":"api","key":"or"}}`)

	blocks := CollectAPIProviderUsage([]OpenPaneSnapshot{pane}, paneClock())
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want only the pane's own backend: %+v", len(blocks), blocks)
	}
	block := blocks[0]
	if block.BackendID != "openrouter" {
		t.Fatalf("backend = %q", block.BackendID)
	}
	if day := block.Windows[0]; day.Tokens != 42 || day.CostUSD != 0.75 {
		t.Fatalf("24h spend = %v/%v, want the current backend's 42/0.75", day.Tokens, day.CostUSD)
	}
}

func TestCollectAPIProviderUsage_KiloGatewayPaneProducesNoBurnBlock(t *testing.T) {
	// A pane on the Kilo Gateway is an allowance, not a burn: it is not classified
	// pay-as-you-go, so it must not drag a spend block into the panel next to the
	// Pass window.
	pane := seedKiloPaneSpend(t, "kilo", []string{kiloTurn("kilo", 9000, 100, 1.25)}, nil,
		`{"kilo":{"type":"oauth","access":"tok"}}`)

	for _, block := range CollectAPIProviderUsage([]OpenPaneSnapshot{pane}, paneClock()) {
		if block.BackendID == "kilo" {
			t.Fatalf("the gateway produced a spend block: %+v", block)
		}
	}
}

func TestCollectAPIProviderUsage_AnAmbiguousKiloPaneContributesNoSpend(t *testing.T) {
	// Two live panes in one repository leave the directory ambiguous, and ambiguity
	// attributes nothing. A pane that cannot be attributed must not borrow the other
	// pane's spend to fill a block — and must not contribute a share of one either,
	// even though the backend's account-wide total is still its own.
	auth := `{"kilo":{"type":"oauth","access":"tok"},"deepseek":{"type":"api","key":"dk"}}`

	// One session in the directory: the pane resolves, and the block names it.
	seedKiloSharedDirectory(t, "deepseek", []string{"ses_one"}, auth)
	resolvable := OpenPaneSnapshot{
		PaneID: "w1:p1", Agent: "kilo", Label: "kilo",
		SessionID: strPtrForPane("ses_one"), Cwd: strPtrForPane("/repo"),
	}
	share := paneShareForKilo(t, resolvable)
	if share == "" {
		t.Fatal("a resolvable pane contributed no share to its backend block")
	}

	// Two sessions: the directory is ambiguous, so the pane has no session at all.
	seedKiloSharedDirectory(t, "deepseek", []string{"ses_one", "ses_two"}, auth)
	sessionID := "ses_gone"
	pane := OpenPaneSnapshot{
		PaneID: "w1:p1", Agent: "kilo", Label: "kilo",
		SessionID: &sessionID, Cwd: strPtrForPane("/repo"),
	}
	if backend := kiloPaneBackendID(pane); backend != "" {
		t.Fatalf("an unattributable pane named the backend %q", backend)
	}
	if tokens, costUSD := PaneTotalUsage("kilo", pane, paneClock()); tokens != 0 || costUSD != 0 {
		t.Fatalf("an unattributable pane reported spend: %v/%v", tokens, costUSD)
	}
	for _, block := range CollectAPIProviderUsage([]OpenPaneSnapshot{pane}, paneClock()) {
		if block.BackendID == "deepseek" && block.PaneActivity != nil {
			t.Fatalf("an unattributable pane claimed a share of a shared backend: %+v", block)
		}
	}
}

// paneShareForKilo is the pane share the Kilo backend block gives this pane.
func paneShareForKilo(t *testing.T, pane OpenPaneSnapshot) string {
	t.Helper()
	for _, block := range CollectAPIProviderUsage([]OpenPaneSnapshot{pane}, paneClock()) {
		if block.BackendID == "deepseek" && block.PaneActivity != nil {
			for _, share := range block.PaneActivity.Panes {
				if share.PaneID == pane.PaneID {
					return share.Label
				}
			}
		}
	}
	return ""
}
