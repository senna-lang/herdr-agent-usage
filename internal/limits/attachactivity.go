/**
 * For each open pane, aggregates the per-provider token activity share
 * over the smallest window. Pure core with injectable deps.
 */
package limits

// OpenPaneSnapshot is one open agent pane used for share aggregation.
type OpenPaneSnapshot struct {
	PaneID    string
	Agent     string
	Label     string
	SessionID *string
	Cwd       *string
}

// agent id -> ProviderLimits.providerId
var agentToProvider = map[string]string{
	"claude":   "claude",
	"codex":    "codex",
	"opencode": "opencode",
	"omp":      "omp",
	"pi":       "pi",
	"grok":     "grok",
}

// PaneActivityDeps injects token collectors for tests and I/O adapters.
type PaneActivityDeps struct {
	TokensForPane func(providerID string, pane OpenPaneSnapshot, windowStartMs, windowEndMs int64) float64
	// TotalTokensForProvider is total tokens across all sessions on disk (open + closed).
	TotalTokensForProvider func(providerID string, windowStartMs, windowEndMs int64) float64
}

// AttachPaneActivity attaches paneActivity by combining open panes with each provider's limits.
func AttachPaneActivity(
	providers []ProviderLimits,
	openPanes []OpenPaneSnapshot,
	nowMs int64,
	deps PaneActivityDeps,
) []ProviderLimits {
	out := make([]ProviderLimits, len(providers))
	for i, p := range providers {
		out[i] = p
		var agentID string
		for a, providerID := range agentToProvider {
			if providerID == p.ProviderID {
				agentID = a
				break
			}
		}
		if agentID == "" {
			continue
		}

		var panesForProvider []OpenPaneSnapshot
		for _, pane := range openPanes {
			if pane.Agent == agentID {
				panesForProvider = append(panesForProvider, pane)
			}
		}
		if len(panesForProvider) == 0 {
			continue
		}

		var primaryWM *int
		if p.Primary != nil {
			primaryWM = p.Primary.WindowMinutes
		}
		windowMinutes := ResolveActivityWindowMinutes(primaryWM)
		startMs := WindowStartMs(nowMs, windowMinutes)
		endMs := nowMs

		rawRows := make([]PaneTokenRow, len(panesForProvider))
		for j, pane := range panesForProvider {
			tokens := 0.0
			if deps.TokensForPane != nil {
				tokens = deps.TokensForPane(p.ProviderID, pane, startMs, endMs)
			}
			rawRows[j] = PaneTokenRow{PaneID: pane.PaneID, Label: pane.Label, Tokens: tokens}
		}
		rows := DisambiguateLabels(rawRows)

		providerTotal := 0.0
		if deps.TotalTokensForProvider != nil {
			providerTotal = deps.TotalTokensForProvider(p.ProviderID, startMs, endMs)
		}
		totalTokens, panes := ComputeSharesWithOther(rows, providerTotal)
		if len(panes) == 0 {
			continue
		}
		// Convert float total to activity type - ProviderPaneActivity uses int total in TS
		// PaneActivityShare uses float tokens; TotalTokens stays int for display.
		activity := ProviderPaneActivity{
			WindowMinutes: windowMinutes,
			TotalTokens:   int(totalTokens),
			Panes:         panes,
		}
		out[i].PaneActivity = &activity
	}
	return out
}
