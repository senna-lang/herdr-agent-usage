/**
 * Collects the rate-limit toasts that are due now, for a host that posts
 * them itself (`usagebar notify --json`, used by the herdr-app mod) instead
 * of the herdr CLI.
 *
 * It runs the same bucket decisions, under the same locks and state files,
 * as herdr delivery: Claude windows per profile state dir (shared with the
 * statusLine bridge), every other provider's shortest window in the shared
 * provider state. A toast returned here is recorded as shown, so it is never
 * returned (or sent through herdr) twice.
 */
package limits

import (
	"strconv"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/ratelimit"
)

// ToastLevel is the severity a host shows a toast with.
type ToastLevel string

const (
	ToastWarning ToastLevel = "warning"
	ToastError   ToastLevel = "error"
)

// toastErrorAtOrBelow is the remaining-percent bucket from which a toast is
// an error rather than a warning (default buckets: 50/20 warn, 10/5 error).
const toastErrorAtOrBelow = 10

// PendingToast is one due toast.
type PendingToast struct {
	ProviderID   string `json:"providerId"`
	ProfileLabel string `json:"profileLabel"`
	// Window is the slot that crossed a threshold: "primary" or "secondary".
	Window   string     `json:"window"`
	Bucket   int        `json:"bucket"`
	ResetsAt int64      `json:"resetsAt"`
	Level    ToastLevel `json:"level"`
	Title    string     `json:"title"`
	Body     string     `json:"body"`
}

// PendingToastsReport is the `notify --json` document.
type PendingToastsReport struct {
	Version int            `json:"version"`
	Toasts  []PendingToast `json:"toasts"`
}

// toastLevelFor maps a fired remaining-percent bucket to a level.
func toastLevelFor(bucket ratelimit.Bucket) (int, ToastLevel) {
	n, err := strconv.Atoi(string(bucket))
	if err != nil || n <= toastErrorAtOrBelow {
		return n, ToastError
	}
	return n, ToastWarning
}

// CollectPendingToasts decides and records every toast due for providers.
func CollectPendingToasts(providers []ProviderLimits, nowMs int64, thresholds []int, percent core.LimitPercent) PendingToastsReport {
	claudeProfiles := ResolvedClaudeProfiles()
	toasts := collectClaudeToasts(providers, claudeProfiles, nowMs, thresholds, percent)
	ratelimit.WithLockedProviderState(func(current ratelimit.ProviderNotifyStateMap) ratelimit.ProviderNotifyStateMap {
		cur := ProviderNotifyState{}
		for k, v := range current {
			cur[k] = v
		}
		next := checkProviderPrimaryLimitsWith(providers, cur, nowMs, thresholds, func(p ProviderLimits) ratelimit.DeliverFn {
			return func(bucket ratelimit.Bucket, text ratelimit.NotificationText) bool {
				toasts = append(toasts, pendingToast(p, "primary", bucket, *p.Primary.ResetsAt, text))
				return true
			}
		}, claudeProfiles, percent)
		out := ratelimit.ProviderNotifyStateMap{}
		for k, v := range next {
			out[k] = v
		}
		return out
	})
	if toasts == nil {
		toasts = []PendingToast{}
	}
	return PendingToastsReport{Version: CollectReportVersion, Toasts: toasts}
}

// collectClaudeToasts applies the statusLine bridge's per-window decisions to
// each Claude profile's collected windows (5h primary, 7d secondary), in that
// profile's own state dir, so the bridge and this path never both fire.
func collectClaudeToasts(providers []ProviderLimits, profiles []claude.ClaudeProfile, nowMs int64, thresholds []int, percent core.LimitPercent) []PendingToast {
	var toasts []PendingToast
	for _, p := range providers {
		profile, ok := profileByIDIn(profiles, p.ProviderID)
		if !ok || p.SameAccountAs != "" {
			continue
		}
		fiveHour, sevenDay := rateWindowInput(p.Primary), rateWindowInput(p.Secondary)
		if fiveHour == nil && sevenDay == nil {
			continue
		}
		account := claude.ToastAccountLabel(profile, profiles)
		deliver := func(slot string, input *ratelimit.RateWindowInput) ratelimit.DeliverFn {
			return func(bucket ratelimit.Bucket, text ratelimit.NotificationText) bool {
				text.Title = ratelimit.WithAccountLabel(text.Title, account)
				toasts = append(toasts, pendingToast(p, slot, bucket, input.ResetsAt, text))
				return true
			}
		}
		ratelimit.WithLockedStateIn(profile.StateDir, func(current ratelimit.ClaudeNotifyState) ratelimit.ClaudeNotifyState {
			return ratelimit.ClaudeNotifyState{
				FiveHour: ratelimit.ProcessWindowWith(ratelimit.WindowFiveHour, fiveHour, current.FiveHour, nowMs, thresholds, deliver("primary", fiveHour), percent),
				SevenDay: ratelimit.ProcessWindowWith(ratelimit.WindowSevenDay, sevenDay, current.SevenDay, nowMs, thresholds, deliver("secondary", sevenDay), percent),
			}
		})
	}
	return toasts
}

// rateWindowInput adapts a collected window to the bucket decision's input;
// a window without a reset time cannot be bucketed and yields nil.
func rateWindowInput(w *LimitWindow) *ratelimit.RateWindowInput {
	if w == nil || w.ResetsAt == nil {
		return nil
	}
	return &ratelimit.RateWindowInput{UsedPercentage: w.UsedPercentage, ResetsAt: *w.ResetsAt}
}

func pendingToast(p ProviderLimits, slot string, bucket ratelimit.Bucket, resetsAt int64, text ratelimit.NotificationText) PendingToast {
	n, level := toastLevelFor(bucket)
	return PendingToast{
		ProviderID:   p.ProviderID,
		ProfileLabel: p.ProfileLabel,
		Window:       slot,
		Bucket:       n,
		ResetsAt:     resetsAt,
		Level:        level,
		Title:        text.Title,
		Body:         text.Body,
	}
}
