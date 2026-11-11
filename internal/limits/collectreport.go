/**
 * The versioned JSON contract of `usagebar collect --format v1`.
 *
 * The legacy `collect` output marshals ProviderLimits as-is, so its field
 * names are Go identifiers and move with refactors. This view is a separate,
 * explicitly tagged document for programs that render the panel themselves
 * (the herdr-app mod): it carries the same facts plus the derived values the
 * text panel computes (window tag, remaining percent, colour band, reset and
 * run-out wording, status), so every renderer shows the same words.
 *
 * Adding a field is compatible; renaming or removing one requires bumping
 * CollectReportVersion.
 */
package limits

import (
	"github.com/senna-lang/herdr-agent-usage/internal/bar"
	"github.com/senna-lang/herdr-agent-usage/internal/core"
)

// CollectReportVersion is the contract version written as "version".
const CollectReportVersion = 1

// CollectFilter names which providers a report was collected for.
type CollectFilter string

const (
	// CollectFilterOpenPanes keeps providers with an open agent pane.
	CollectFilterOpenPanes CollectFilter = "open-panes"
	// CollectFilterAll keeps every configured provider.
	CollectFilterAll CollectFilter = "all"
)

// CollectReport is the top-level v1 document.
type CollectReport struct {
	Version       int                 `json:"version"`
	GeneratedAtMs int64               `json:"generatedAtMs"`
	Filter        CollectFilter       `json:"filter"`
	LimitPercent  core.LimitPercent   `json:"limitPercent"`
	EmptyMessage  string              `json:"emptyMessage"`
	Providers     []ProviderReport    `json:"providers"`
	APIUsage      []APIUsageReport    `json:"apiUsage"`
	LowCachePanes []LowCachePaneEntry `json:"lowCachePanes"`
	// WatchDirs are folders whose changes mean fresh data (see WatchDirs).
	WatchDirs []string `json:"watchDirs"`
}

// ProviderReport is one subscription account.
type ProviderReport struct {
	ProviderID   string `json:"providerId"`
	Family       string `json:"family"`
	Label        string `json:"label"`
	GroupLabel   string `json:"groupLabel"`
	AccountLabel string `json:"accountLabel"`
	ProfileID    string `json:"profileId"`
	ProfileLabel string `json:"profileLabel"`
	Identity     string `json:"identity"`
	// Account is the login (email or account id) when readable, and
	// SameAccountAs the providerId of the earlier entry for that account.
	Account       string `json:"account"`
	SameAccountAs string `json:"sameAccountAs"`
	// Home is the account's config/home dir, where its agent writes readings.
	Home string `json:"home"`
	// InUse is false for a provider with nothing on this machine (no
	// install, no profile, no data); Configured marks an explicit profile.
	InUse        bool    `json:"inUse"`
	Configured   bool    `json:"configured"`
	PlanType     *string `json:"planType"`
	Source       string  `json:"source"`
	FetchedAtMs  int64   `json:"fetchedAtMs"`
	Note         *string `json:"note"`
	Status       Status  `json:"status"`
	StaleMinutes *int    `json:"staleMinutes"`
	Action       string  `json:"action"`
	// Windows are in display order (primary, secondary, tertiary) and hold
	// only the windows the provider reported.
	Windows      []WindowReport      `json:"windows"`
	PaneActivity *PaneActivityReport `json:"paneActivity"`
}

// WindowReport is one limit window with its display values precomputed.
type WindowReport struct {
	// Slot is "primary", "secondary" or "tertiary".
	Slot          string  `json:"slot"`
	Tag           string  `json:"tag"`
	WindowMinutes *int    `json:"windowMinutes"`
	UsedPercent   float64 `json:"usedPercentage"`
	// RemainingPercent is 0-100, rounded; the colour band follows it even
	// when the user shows used percentages.
	RemainingPercent int `json:"remainingPercent"`
	// DisplayPercent is the number to print under LimitPercent, and BarFill
	// the bar fill fraction (0-1) that goes with it.
	DisplayPercent int          `json:"displayPercent"`
	BarFill        float64      `json:"barFill"`
	Band           bar.BarTone  `json:"band"`
	ResetsAt       *int64       `json:"resetsAt"`
	ResetIn        string       `json:"resetIn"`
	RunOut         *RunOutEntry `json:"runOut"`
}

// RunOutEntry is the run-out projection; Text is set only when it warns.
type RunOutEntry struct {
	MinutesToEmpty   float64 `json:"minutesToEmpty"`
	EmptyBeforeReset bool    `json:"emptyBeforeReset"`
	Text             string  `json:"text"`
}

// PaneActivityReport is per-pane token share over the smallest window.
type PaneActivityReport struct {
	WindowMinutes int              `json:"windowMinutes"`
	Tag           string           `json:"tag"`
	TotalTokens   int              `json:"totalTokens"`
	Panes         []PaneShareEntry `json:"panes"`
}

// PaneShareEntry is one pane's share.
type PaneShareEntry struct {
	PaneID       string  `json:"paneId"`
	Label        string  `json:"label"`
	Tokens       float64 `json:"tokens"`
	SharePercent float64 `json:"sharePercent"`
}

// APIUsageReport is one pay-as-you-go backend.
type APIUsageReport struct {
	BackendID    string              `json:"backendId"`
	Label        string              `json:"label"`
	HasCost      bool                `json:"hasCost"`
	Windows      []APIWindowEntry    `json:"windows"`
	Models       []APIModelEntry     `json:"models"`
	PaneActivity *PaneActivityReport `json:"paneActivity"`
}

// APIWindowEntry is one rolling spend window.
type APIWindowEntry struct {
	WindowMinutes int     `json:"windowMinutes"`
	Tag           string  `json:"tag"`
	Tokens        float64 `json:"tokens"`
	CostUSD       float64 `json:"costUsd"`
}

// APIModelEntry is one model's 24h contribution.
type APIModelEntry struct {
	ModelID string  `json:"modelId"`
	Tokens  float64 `json:"tokens"`
	CostUSD float64 `json:"costUsd"`
}

// LowCachePaneEntry is a pane with a red-band prompt-cache hit rate.
type LowCachePaneEntry struct {
	Label      string  `json:"label"`
	HitPercent float64 `json:"hitPercent"`
}

// CollectReportInput is everything one report is built from.
type CollectReportInput struct {
	Providers     []ProviderLimits
	APIUsage      []APIProviderUsage
	LowCachePanes []LowCachePane
	WatchDirs     []string
	Filter        CollectFilter
	LimitPercent  core.LimitPercent
	NowMs         int64
}

// BuildCollectReport converts a collected snapshot into the v1 document.
// Slices are never nil so consumers can iterate without null checks.
func BuildCollectReport(in CollectReportInput) CollectReport {
	percent := in.LimitPercent
	if percent == "" {
		percent = core.LimitPercentRemaining
	}
	report := CollectReport{
		Version:       CollectReportVersion,
		GeneratedAtMs: in.NowMs,
		Filter:        in.Filter,
		LimitPercent:  percent,
		EmptyMessage:  emptyMessageFor(in.Filter),
		Providers:     make([]ProviderReport, 0, len(in.Providers)),
		APIUsage:      make([]APIUsageReport, 0, len(in.APIUsage)),
		LowCachePanes: make([]LowCachePaneEntry, 0, len(in.LowCachePanes)),
		WatchDirs:     append([]string{}, in.WatchDirs...),
	}
	for _, p := range in.Providers {
		report.Providers = append(report.Providers, providerReport(p, percent, in.NowMs))
	}
	for _, u := range in.APIUsage {
		report.APIUsage = append(report.APIUsage, apiUsageReport(u))
	}
	for _, pane := range in.LowCachePanes {
		report.LowCachePanes = append(report.LowCachePanes, LowCachePaneEntry(pane))
	}
	return report
}

// emptyMessageFor is the text panel's empty-list wording for each filter.
func emptyMessageFor(filter CollectFilter) string {
	if filter == CollectFilterOpenPanes {
		return "(no agent panes open)"
	}
	return "(no usage data yet)"
}

func providerReport(p ProviderLimits, percent core.LimitPercent, nowMs int64) ProviderReport {
	out := ProviderReport{
		ProviderID:    p.ProviderID,
		Family:        p.Family,
		Label:         p.Label,
		GroupLabel:    p.GroupLabel,
		AccountLabel:  p.AccountLabel,
		ProfileID:     p.ProfileID,
		ProfileLabel:  p.ProfileLabel,
		Identity:      p.Identity,
		Account:       p.Account,
		Home:          p.Home,
		InUse:         p.InUse,
		Configured:    p.Configured,
		SameAccountAs: p.SameAccountAs,
		PlanType:      p.PlanType,
		Source:        p.Source,
		FetchedAtMs:   p.FetchedAtMs,
		Note:          p.Note,
		Status:        p.Status(),
		StaleMinutes:  p.StaleMinutes,
		Action:        p.Action,
		Windows:       make([]WindowReport, 0, 3),
	}
	// Same slots and fallback tags as the text panel's richBlock.
	slots := []struct {
		name, fallback string
		window         *LimitWindow
	}{
		{"primary", "5h", p.Primary},
		{"secondary", "7d", p.Secondary},
		{"tertiary", "30d", p.Tertiary},
	}
	for _, slot := range slots {
		if slot.window != nil {
			out.Windows = append(out.Windows, windowReport(slot.name, windowTag(slot.window, slot.fallback), *slot.window, percent, nowMs))
		}
	}
	if p.PaneActivity != nil {
		out.PaneActivity = paneActivityReport(*p.PaneActivity)
	}
	return out
}

func windowReport(slot, tag string, w LimitWindow, percent core.LimitPercent, nowMs int64) WindowReport {
	rem := remainingOf(w.UsedPercentage)
	out := WindowReport{
		Slot:             slot,
		Tag:              tag,
		WindowMinutes:    w.WindowMinutes,
		UsedPercent:      w.UsedPercentage,
		RemainingPercent: rem,
		DisplayPercent:   percent.DisplayPercent(rem),
		BarFill:          percent.BarFill(rem) / 100,
		Band:             bar.ToneForRemaining(float64(rem)),
		ResetsAt:         w.ResetsAt,
	}
	if w.ResetsAt != nil && *w.ResetsAt > 0 {
		out.ResetIn = formatResetIn(*w.ResetsAt*1000 - nowMs)
	}
	if w.RunOut != nil {
		out.RunOut = &RunOutEntry{MinutesToEmpty: w.RunOut.MinutesToEmpty, EmptyBeforeReset: w.RunOut.EmptyBeforeReset}
		if w.RunOut.EmptyBeforeReset {
			out.RunOut.Text = "empty in ~" + formatShortDuration(w.RunOut.MinutesToEmpty)
		}
	}
	return out
}

func paneActivityReport(a ProviderPaneActivity) *PaneActivityReport {
	out := &PaneActivityReport{
		WindowMinutes: a.WindowMinutes,
		Tag:           minutesTag(a.WindowMinutes),
		TotalTokens:   a.TotalTokens,
		Panes:         make([]PaneShareEntry, 0, len(a.Panes)),
	}
	for _, pane := range a.Panes {
		out.Panes = append(out.Panes, PaneShareEntry(pane))
	}
	return out
}

func apiUsageReport(u APIProviderUsage) APIUsageReport {
	out := APIUsageReport{
		BackendID: u.BackendID,
		Label:     u.Label,
		HasCost:   u.HasCost,
		Windows:   make([]APIWindowEntry, 0, len(u.Windows)),
		Models:    make([]APIModelEntry, 0, len(u.Models)),
	}
	for _, w := range u.Windows {
		out.Windows = append(out.Windows, APIWindowEntry{WindowMinutes: w.WindowMinutes, Tag: minutesTag(w.WindowMinutes), Tokens: w.Tokens, CostUSD: w.CostUSD})
	}
	for _, m := range u.Models {
		out.Models = append(out.Models, APIModelEntry(m))
	}
	if u.PaneActivity != nil {
		out.PaneActivity = paneActivityReport(*u.PaneActivity)
	}
	return out
}
