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

// agent id -> ProviderLimits.providerId. Used as-is for the single-Claude-
// profile default; when multiple Claude profiles are configured, claude
// resolution instead goes through an injected PaneProviderResolver that can
// tell profiles apart (see BuildClaudePaneProviderResolver), since "claude"
// alone cannot say which profile a pane belongs to.
var agentToProvider = map[string]string{
	"claude":   "claude",
	"codex":    "codex",
	"opencode": "opencode",
	"grok":     "grok",
}

// PaneProviderResolver maps an open pane to the provider id its activity
// should be attributed to. ok=false means unresolved (e.g. a Claude pane whose
// session doesn't match any configured profile) — the pane is then excluded
// from every provider's activity rather than guessed into one.
type PaneProviderResolver func(pane OpenPaneSnapshot) (providerID string, ok bool)

// defaultPaneProviderResolver is an exact agent-id match. Used when
// PaneActivityDeps.ResolvePaneProvider is nil, which keeps pre-multi-profile
// callers (and their tests) unchanged.
func defaultPaneProviderResolver(pane OpenPaneSnapshot) (string, bool) {
	id, ok := agentToProvider[pane.Agent]
	return id, ok
}

// PaneActivityDeps injects token collectors for tests and I/O adapters.
type PaneActivityDeps struct {
	TokensForPane func(providerID string, pane OpenPaneSnapshot, windowStartMs, windowEndMs int64) float64
	// TotalTokensForProvider is total tokens across all sessions on disk (open + closed).
	TotalTokensForProvider func(providerID string, windowStartMs, windowEndMs int64) float64
	// ResolvePaneProvider attributes a pane to a provider id. nil defaults to
	// defaultPaneProviderResolver (today's single-Claude-profile behavior).
	ResolvePaneProvider PaneProviderResolver
}

// AttachPaneActivity attaches paneActivity by combining open panes with each provider's limits.
func AttachPaneActivity(
	providers []ProviderLimits,
	openPanes []OpenPaneSnapshot,
	nowMs int64,
	deps PaneActivityDeps,
) []ProviderLimits {
	resolve := deps.ResolvePaneProvider
	if resolve == nil {
		resolve = defaultPaneProviderResolver
	}
	// Resolve each pane once (not once per provider): a claude-family resolver
	// may scan disk, so this keeps cost O(panes) instead of O(providers*panes).
	paneProviderID := make(map[string]string, len(openPanes))
	for _, pane := range openPanes {
		if id, ok := resolve(pane); ok {
			paneProviderID[pane.PaneID] = id
		}
	}

	out := make([]ProviderLimits, len(providers))
	for i, p := range providers {
		out[i] = p

		var panesForProvider []OpenPaneSnapshot
		for _, pane := range openPanes {
			if paneProviderID[pane.PaneID] == p.ProviderID {
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
