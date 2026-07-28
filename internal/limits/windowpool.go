/**
 * Account-keyed pool of rate-limit window observations.
 *
 * A rate-limit window belongs to the ACCOUNT, not to the agent that happened
 * to observe it. codexlimits.go already states this for Codex ("rate limits
 * are account-wide: every session records the same primary/secondary
 * window"), and it holds for every provider here. Yet each collector binds
 * its data source to one vendor CLI's private files, so an account driven
 * through another harness reports "no data" even when its real windows sit
 * on disk, recorded by whoever did make the request.
 *
 * This pool closes that gap: observations are keyed by (provider, account),
 * and a collector whose own sources are empty borrows the newest observation
 * for ITS account from any observer.
 *
 * Two rules are load-bearing, because a wrong borrow is worse than an honest
 * blank:
 *   1. The account join is strict — a borrow that cannot be shown to concern
 *      the same account is refused (SelectAccountWindows).
 *   2. A borrowed row always names its observer, its account (or says the
 *      account is unverified), and its age (BorrowedProviderLimits). It must
 *      never render as first-hand current data.
 */
package limits

import (
	"sort"
	"strings"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/omp"
)

// AccountWindows is one agent's observation of one account's windows.
type AccountWindows struct {
	// ProviderID is the collector id this observation feeds (claude, codex,
	// grok, opencode) — not the harness that recorded it.
	ProviderID string
	// AccountKey is the observer's raw account key. It is NOT an identity:
	// OMP composes it from the credential shape, so one account appears under
	// several keys ("oauth|account:…|email:…" and "oauth|secret:…").
	AccountKey string
	// AccountID and Email are the stable identity, when the observer has one.
	AccountID string
	Email     string
	Primary   *LimitWindow
	Secondary *LimitWindow
	Tertiary  *LimitWindow
	// ObservedAtMs is when the observing agent saw these numbers.
	ObservedAtMs int64
	// Observer names who recorded it, for display attribution.
	Observer string
}

// hasWindow reports whether the observation carries anything displayable.
func (a AccountWindows) hasWindow() bool {
	return a.Primary != nil || a.Secondary != nil || a.Tertiary != nil
}

// identity collapses the observer's keys to one stable account identity.
func (a AccountWindows) identity() string {
	if a.AccountID != "" {
		return a.AccountID
	}
	if a.Email != "" {
		return strings.ToLower(a.Email)
	}
	return a.AccountKey
}

// identified reports whether the observer named the account at all. An
// unidentified observation can be neither confirmed nor refuted against a
// wanted account.
func (a AccountWindows) identified() bool {
	return a.AccountID != "" || a.Email != ""
}

// identifies reports whether this observation belongs to account want, which
// may be an email or an account id.
func (a AccountWindows) identifies(want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return false
	}
	if strings.EqualFold(a.Email, want) || strings.EqualFold(a.AccountID, want) {
		return true
	}
	// OMP embeds both inside its composed key.
	return strings.Contains(strings.ToLower(a.AccountKey), want)
}

// windowSlot is where a provider's window belongs in the display triple.
type windowSlot int

const (
	slotNone windowSlot = iota
	slotPrimary
	slotSecondary
	slotTertiary
)

// slotForLimitID maps an OMP limit_id to a display slot and fallback duration.
//
// The window vocabulary differs per provider, so the mapping is explicit
// rather than pattern-matched: an unrecognized id is skipped, never guessed
// into the wrong bar. A recognized persisted window label overrides the
// fallback duration when observations are folded below.
func slotForLimitID(providerID, limitID string) (windowSlot, int) {
	switch providerID {
	case "claude":
		switch limitID {
		case "anthropic:5h":
			return slotPrimary, 300
		case "anthropic:7d":
			return slotSecondary, 10080
		}
		// anthropic:extra carries no window semantics the panel can render.
	case "codex":
		switch {
		case strings.HasSuffix(limitID, ":primary"):
			return slotPrimary, 300
		case strings.HasSuffix(limitID, ":secondary"):
			return slotSecondary, 10080
		}
	case "grok":
		switch limitID {
		// The plan meter, matching the single window CollectGrokLimits shows.
		case "xai-oauth:credits:1w":
			return slotPrimary, 10080
		case "xai-oauth:included:1mo":
			return slotSecondary, 43200
		}
		// xai-oauth:product:* are per-product sub-meters with no display slot.
	case "opencode":
		switch limitID {
		case "rolling-5h":
			return slotPrimary, 300
		case "weekly":
			return slotSecondary, 10080
		case "monthly":
			return slotTertiary, 43200
		}
	}
	return slotNone, 0
}

// windowMinutesFromLabel returns the duration explicitly recorded by OMP.
// Provider limit ids such as openai-codex:primary are ordinals, not durations:
// the same primary id may represent the only 7-day window for an account.
func windowMinutesFromLabel(label string) int {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "5h", "5 hour", "5 hours":
		return 300
	case "7d", "7 day", "7 days", "weekly":
		return 10080
	case "30d", "30 day", "30 days", "monthly":
		return 43200
	default:
		return 0
	}
}

// windowFromUsageFraction converts an OMP row to a display window. The
// fraction may exceed 1 for an exhausted window; remaining cannot go
// negative, so it clamps at fully used. resets_at is epoch ms upstream and
// epoch seconds in LimitWindow.
func windowFromUsageFraction(usedFraction float64, resetsAtMs int64, windowMinutes int) *LimitWindow {
	used := usedFraction * 100
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	w := &LimitWindow{UsedPercentage: used}
	if windowMinutes > 0 {
		m := windowMinutes
		w.WindowMinutes = &m
	}
	if resetsAtMs > 0 {
		sec := resetsAtMs / 1000
		w.ResetsAt = &sec
	}
	return w
}

// AccountWindowsFromOMP folds OMP's usage_history rows into per-account
// observations.
//
// Rows reach a collector through the one shared route table
// (SubscriptionRouteForProviderAuth): a usage_history row only exists for a
// subscription account, which is what "oauth" asserts there. Providers the
// table does not route are skipped rather than guessed.
func AccountWindowsFromOMP(rows []omp.UsageWindow) []AccountWindows {
	// Oldest first, so a newer row for the same slot overwrites an older one.
	ordered := make([]omp.UsageWindow, len(rows))
	copy(ordered, rows)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].RecordedAtMs < ordered[j].RecordedAtMs
	})

	type key struct{ provider, account string }
	index := make(map[key]*AccountWindows)
	order := make([]key, 0, len(ordered))

	for _, row := range ordered {
		route, ok := SubscriptionRouteForProviderAuth(row.Provider, "oauth")
		if !ok {
			continue
		}
		slot, minutes := slotForLimitID(route.CollectorProviderID, row.LimitID)
		if observedMinutes := windowMinutesFromLabel(row.WindowLabel); observedMinutes > 0 {
			minutes = observedMinutes
		}
		if slot == slotNone {
			continue
		}
		candidate := AccountWindows{
			ProviderID: route.CollectorProviderID,
			AccountKey: row.AccountKey,
			AccountID:  row.AccountID,
			Email:      row.Email,
			Observer:   "OMP",
		}
		k := key{route.CollectorProviderID, candidate.identity()}
		acc, seen := index[k]
		if !seen {
			acc = &candidate
			index[k] = acc
			order = append(order, k)
		}
		if acc.Email == "" {
			acc.Email = row.Email
		}
		if acc.AccountID == "" {
			acc.AccountID = row.AccountID
		}
		window := windowFromUsageFraction(row.UsedFraction, row.ResetsAtMs, minutes)
		switch slot {
		case slotPrimary:
			acc.Primary = window
		case slotSecondary:
			acc.Secondary = window
		case slotTertiary:
			acc.Tertiary = window
		}
		if row.RecordedAtMs > acc.ObservedAtMs {
			acc.ObservedAtMs = row.RecordedAtMs
		}
	}

	out := make([]AccountWindows, 0, len(order))
	for _, k := range order {
		out = append(out, *index[k])
	}
	return out
}

// SelectAccountWindows picks the observation a collector may borrow for
// providerID, or nil when borrowing cannot be shown to concern the right
// account.
//
// wantAccount is the identity the pane bills to, empty when the collector
// cannot name it. The rules, in order:
//   - An exact identity match always wins.
//   - Identities exist for this provider but none match the wanted account →
//     refuse. Showing account A's numbers on a pane billing account B is
//     worse than showing nothing.
//   - Nobody named the account (the observer records some providers by
//     opaque key only) → borrow only when a single account was observed, and
//     let BorrowedProviderLimits label it unverified.
func SelectAccountWindows(observations []AccountWindows, providerID, wantAccount string) *AccountWindows {
	var candidates []AccountWindows
	for _, o := range observations {
		if o.ProviderID == providerID && o.hasWindow() {
			candidates = append(candidates, o)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	if strings.TrimSpace(wantAccount) != "" {
		var matched []AccountWindows
		anyIdentified := false
		for _, c := range candidates {
			if c.identified() {
				anyIdentified = true
			}
			if c.identifies(wantAccount) {
				matched = append(matched, c)
			}
		}
		switch {
		case len(matched) > 0:
			candidates = matched
		case anyIdentified:
			return nil
		default:
			if !singleAccount(candidates) {
				return nil
			}
		}
	} else if !singleAccount(candidates) {
		return nil
	}

	newest := candidates[0]
	for _, c := range candidates[1:] {
		if c.ObservedAtMs > newest.ObservedAtMs {
			newest = c
		}
	}
	return &newest
}

// singleAccount reports whether every candidate is the same account.
func singleAccount(candidates []AccountWindows) bool {
	first := candidates[0].identity()
	for _, c := range candidates[1:] {
		if c.identity() != first {
			return false
		}
	}
	return true
}

// observationAge renders how long ago an observation was taken.
func observationAge(observedAtMs, nowMs int64) string {
	minutes := int((nowMs - observedAtMs) / 60_000)
	if minutes < 1 {
		return "just now"
	}
	if minutes < 60 {
		return "~" + itoa(minutes) + "m ago"
	}
	return "~" + itoa(minutes/60) + "h ago"
}

// BorrowedProviderLimits renders another agent's observation as this
// provider's row. Attribution, account and age are not optional: without them
// a borrowed number is indistinguishable from a first-hand one.
func BorrowedProviderLimits(o AccountWindows, providerID, label string, nowMs int64) ProviderLimits {
	account := "account unverified"
	if o.Email != "" {
		account = "account " + o.Email
	}
	// An observation with no timestamp is dated "now" so the row is not
	// treated as stale, and its age is reported as unknown rather than
	// computed from the epoch — the row must not claim both at once.
	age := "age unknown"
	fetched := nowMs
	if o.ObservedAtMs > 0 {
		age = observationAge(o.ObservedAtMs, nowMs)
		fetched = o.ObservedAtMs
	}
	note := strings.Join([]string{account, "via " + o.Observer, age}, " · ")
	return ProviderLimits{
		ProviderID:  providerID,
		Label:       label,
		Primary:     o.Primary,
		Secondary:   o.Secondary,
		Tertiary:    o.Tertiary,
		Source:      strings.ToLower(o.Observer) + " usage_history",
		FetchedAtMs: fetched,
		Note:        &note,
	}
}
