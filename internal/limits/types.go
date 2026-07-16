/**
 * Per-provider rate-limit snapshot used for display.
 */
package limits

// RunOutEstimate projects when a window will be exhausted at the recently
// observed consumption pace (approach B). Attached to a window after the
// history pass; the formatter only reads it.
type RunOutEstimate struct {
	// MinutesToEmpty until empty. 0 means already empty.
	MinutesToEmpty float64 `json:"minutesToEmpty"`
	// EmptyBeforeReset is true when projected to empty before its reset (only then warn).
	EmptyBeforeReset bool `json:"emptyBeforeReset"`
}

// LimitWindow is one rate-limit window snapshot.
type LimitWindow struct {
	// UsedPercentage (0-100). Remaining display = 100 - used.
	UsedPercentage float64 `json:"usedPercentage"`
	// ResetsAt is unix epoch seconds. Nil when unknown.
	ResetsAt *int64 `json:"resetsAt,omitempty"`
	// WindowMinutes hints the window length for display labels.
	WindowMinutes *int `json:"windowMinutes,omitempty"`
	// RunOut projection from the recent-pace history pass. Nil when there is
	// not enough history, the pace is flat/negative, or it holds until reset.
	RunOut *RunOutEstimate `json:"runOut,omitempty"`
}

// ProviderLimits is a per-provider rate-limit snapshot used for display.
type ProviderLimits struct {
	ProviderID string
	Label      string
	// Primary is the short window (5h / session).
	Primary *LimitWindow
	// Secondary is the mid window (weekly).
	Secondary *LimitWindow
	// Tertiary is the long window (monthly). OpenCode Go etc.
	Tertiary    *LimitWindow
	PlanType    *string
	Source      string
	FetchedAtMs int64
	Note        *string
	// PaneActivity is per-pane token activity share over the smallest window.
	PaneActivity *ProviderPaneActivity
}

// ProviderPaneActivity is windowed per-pane activity for one provider.
type ProviderPaneActivity struct {
	// WindowMinutes used for the aggregation.
	WindowMinutes int
	TotalTokens   int
	Panes         []PaneActivityShare
}
