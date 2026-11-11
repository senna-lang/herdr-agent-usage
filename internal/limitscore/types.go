/**
 * Per-provider rate-limit snapshot used for display.
 *
 * Lives below internal/limits and every provider package: both sides need
 * ProviderLimits (limits to render it, provider adapters to construct it), so
 * it cannot live in internal/limits itself without an import cycle. See
 * windowpool.go for the account-window pool that also lives here for the
 * same reason.
 */
package limitscore

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
	// GroupLabel nests this entry under one shared heading in the panel
	// (e.g. multiple configured Claude accounts under "Claude") instead of
	// its own top-level block. Only takes effect when 2+ entries share the
	// same non-empty GroupLabel; a lone entry renders as if it were empty.
	GroupLabel string
	// AccountLabel is shown in place of Label for the per-account line inside
	// a group (e.g. the account's real login email), so members sharing one
	// GroupLabel stay distinguishable. Ignored outside a group.
	AccountLabel string
	// ProfileID, ProfileLabel and Identity name the configured account this
	// entry was collected for, as separate facts rather than folded into
	// Label/AccountLabel: the id and label from its [[<family>.profiles]]
	// entry (or the family default), and the most specific identity the
	// adapter can show (a login email, else the account's home directory).
	// Empty for providers without per-account profiles.
	ProfileID    string
	ProfileLabel string
	Identity     string
	// StaleMinutes is set when the windows come from an observation older
	// than the adapter's freshness bound (the same age the "stale ~Nm ago"
	// Note reports). Nil means fresh or unknown.
	StaleMinutes *int
	// Action is the command the user must run to restore this provider's
	// data (e.g. "grok login"), when the adapter knows one. Empty otherwise.
	Action string
}

// Status is the provider-neutral health of one ProviderLimits entry, so a
// renderer can choose its look without parsing the free-text Note.
type Status string

const (
	// StatusOK has at least one window and no freshness or login problem.
	StatusOK Status = "ok"
	// StatusStale has windows, but they are older than the freshness bound.
	StatusStale Status = "stale"
	// StatusNeedsLogin needs the user to run Action before data can return.
	StatusNeedsLogin Status = "needs-login"
	// StatusUnavailable has no windows and no known fix (no source yet).
	StatusUnavailable Status = "unavailable"
)

// AllStatuses lists every Status, for exhaustive contract tests.
var AllStatuses = []Status{StatusOK, StatusStale, StatusNeedsLogin, StatusUnavailable}

// HasWindows reports whether any limit window is present.
func (p ProviderLimits) HasWindows() bool {
	return p.Primary != nil || p.Secondary != nil || p.Tertiary != nil
}

// Status derives the entry's health from the structured fields adapters set
// (Action, StaleMinutes, windows); it never inspects Note. A required login
// wins over staleness because old windows cannot refresh until it is done.
func (p ProviderLimits) Status() Status {
	switch {
	case p.Action != "":
		return StatusNeedsLogin
	case !p.HasWindows():
		return StatusUnavailable
	case p.StaleMinutes != nil:
		return StatusStale
	default:
		return StatusOK
	}
}

// ProviderPaneActivity is windowed per-pane activity for one provider.
type ProviderPaneActivity struct {
	// WindowMinutes used for the aggregation.
	WindowMinutes int
	TotalTokens   int
	Panes         []PaneActivityShare
}

// PaneActivityShare is a pane's share of provider activity.
type PaneActivityShare struct {
	PaneID string
	Label  string
	Tokens float64
	// SharePercent is 0-100. Zero when the sum is zero.
	SharePercent float64
}
