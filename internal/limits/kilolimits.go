/**
 * Kilo Code limit collection.
 *
 * Kilo publishes no 5h or 7h bucket for the Kilo Gateway. Its allowance is a
 * monthly credit total, so this collector produces exactly one monthly window
 * and leaves the short windows nil rather than borrowing the monthly number
 * into them. Kilo publishes no rate limit at all — no response carries one, and
 * none is inferred here.
 *
 * Without a Kilo Pass subscription, which is the state most accounts are in,
 * there is no allowance to meter. That is still not empty: the account's credit
 * balance is real and is reported as the note, because a note is the display
 * surface for a fact that is not a window. What is deliberately absent is any
 * percentage. Kilo publishes no limit for a non-subscriber, so a bar drawn
 * against a number that does not exist would be invented rather than measured.
 */
package limits

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/kilo"
)

// kiloMonthlyWindowMinutes is a calendar month's worth of minutes, used only
// as a display hint. The countdown is driven by the reset timestamp Kilo itself
// reports, never by this constant.
const kiloMonthlyWindowMinutes = 30 * 24 * 60

// kiloAPIURL is pinned. The credential is only ever sent to this host, and the
// client refuses to follow a redirect away from it.
//
// Kilo lets KILO_API_URL move the API host for its own runs. This collector
// does not honour it: an account's allowance lives on the official control
// plane, and an override pointing elsewhere would send the login to a host
// this plugin cannot vouch for.
const kiloAPIURL = "https://api.kilo.ai"

// kiloOrganizationHeader is Kilo's scope header for the selected team. Kilo's
// own client sends the login's accountId with every wallet request, so the
// balance this collector reads is the selected team's and not the person's.
const kiloOrganizationHeader = "x-kilocode-organizationid"

// kiloPassLiveStatuses is the set Kilo's own CLI treats as a live subscription.
// Any other status means the plan is not paying for the session.
var kiloPassLiveStatuses = map[string]bool{
	"active":   true,
	"past_due": true,
	"trialing": true,
}

// KiloBalance is the account's prepaid credit pool.
type KiloBalance struct {
	// Balance is the remaining balance in USD.
	Balance float64
	// IsDepleted is Kilo's own flag for "this wallet is empty".
	IsDepleted bool
}

// KiloPassState is the normalised Kilo Pass allowance.
type KiloPassState struct {
	BaseCreditsUSD  float64
	BonusCreditsUSD float64
	UsageUSD        float64
	NextBillingAt   string
	Status          string
	// HasAllowanceParts and HasUsageParts record which halves of the ratio the
	// server actually named. Both are required before a window is produced: a
	// missing spend would read as an untouched period, which presents as a
	// full allowance.
	HasAllowanceParts bool
	HasUsageParts     bool
}

// HasSubscription reports whether the response named a plan at all, as opposed to
// subscription: null — an account paying from a shared credit balance. Any of the
// three plan fields is enough: a plan can be reported by its status alone, and an
// amount with no status is still a plan whose allowance was named.
func (s *KiloPassState) HasSubscription() bool {
	return s != nil && (s.Status != "" || s.HasAllowanceParts || s.HasUsageParts)
}

// HasRatio reports whether the response named both halves of this period's credit
// ratio, which is the only shape that can become a window at all.
func (s *KiloPassState) HasRatio() bool {
	return s != nil && s.HasAllowanceParts && s.HasUsageParts
}

// CollectKiloLimitsOptions injects every dependency, so no test reaches the
// network or the developer's real Kilo account.
type CollectKiloLimitsOptions struct {
	// AuthPath defaults to Kilo's resolved auth.json.
	AuthPath string
	// FetchPass defaults to the real endpoint. A Kilo Pass is personal scope, so
	// it is never read with an organization scope attached.
	FetchPass func(access string) (*KiloPassState, error)
	// FetchBalance defaults to the real endpoint, and takes the selected
	// organization because the wallet it reads is that team's.
	FetchBalance func(access, organizationID string) (*KiloBalance, error)
	// Now defaults to the passed nowMs; tests pin it.
	Now func() int64
}

// kiloPassOutcome is what one fetch of the Kilo Pass state actually established.
//
// The three cases must not be collapsed. Kilo upstream reads a null
// subscription and a non-live status as "no consumable Pass", not as a failed
// request, and that distinction decides whether a pane is cleared or preserved.
type kiloPassOutcome int

const (
	// kiloPassAllowance: a metered window.
	kiloPassAllowance kiloPassOutcome = iota
	// kiloPassNoPass: Kilo answered, and answered that there is nothing to
	// meter. This clears any window this account had.
	kiloPassNoPass
	// kiloPassFailed: the request, the decode, or the shape failed. Kilo said
	// nothing about the account, so nothing is cleared.
	kiloPassFailed
)

// kiloPassQuery is one account's already-fetched answer, plus the scope it was
// fetched for. Grouping them keeps the ladder in kiloProviderLimits about
// reading the facts rather than about where they came from.
type kiloPassQuery struct {
	Pass           *KiloPassState
	PassErr        error
	Balance        *KiloBalance
	BalanceErr     error
	OrganizationID string
}

// CollectKiloLimits builds a ProviderLimits for one Kilo account.
//
// The ladder is deliberate. A window only ever comes from a Kilo Pass
// subscription; balance only ever becomes part of a note; and every dead end
// produces a note naming the cause rather than a fabricated bar. The result is
// cached against the login's identity and scope, so a second account — or the
// same account after a switch to another organization — never sees the
// numbers its predecessor produced.
//
// The cache carries the outcome, the last good reading and the failed attempt
// apart. A failed fetch must record that it happened, so the next refresh does
// not re-hit the endpoint, without either destroying the reading it did not
// disprove or passing that reading off as the result of the failure.
func CollectKiloLimits(nowMs int64, opts CollectKiloLimitsOptions) ProviderLimits {
	const providerID = "kilo"
	const label = "Kilo"

	authPath := opts.AuthPath
	if authPath == "" {
		authPath = kilo.ResolveKiloAuthPath()
	}
	credential := kilo.GatewayLogin(authPath)
	if credential == nil {
		pl := ProviderLimits{ProviderID: providerID, Label: label, Source: "none", FetchedAtMs: nowMs}
		pl.Note = strPtr("no Kilo Gateway login in Kilo's auth.json — run `kilo auth login`")
		return pl
	}

	previous, _ := readKiloCache()
	if cached, ok := cachedKiloLimits(previous, credential.Identity, nowMs); ok {
		return cached
	}

	fetchPass := opts.FetchPass
	if fetchPass == nil {
		fetchPass = fetchKiloPassState
	}
	fetchBalance := opts.FetchBalance
	if fetchBalance == nil {
		fetchBalance = fetchKiloBalance
	}

	balance, balanceErr := fetchBalance(credential.Access, credential.OrganizationID)
	pass, passErr := fetchPass(credential.Access)
	pl, outcome := kiloProviderLimits(providerID, label, kiloPassQuery{
		Pass:           pass,
		PassErr:        passErr,
		Balance:        balance,
		BalanceErr:     balanceErr,
		OrganizationID: credential.OrganizationID,
	}, nowMs)

	entry := kiloCacheEntry{
		FetchedAtMs: nowMs,
		AccountID:   credential.Identity,
		Outcome:     kiloOutcomeFor(outcome),
		Limits:      &pl,
	}
	if outcome == kiloPassFailed {
		// The attempt is recorded in its own right; the reading is not. A blip
		// at the endpoint says nothing about whether the account still has its
		// Pass, so the last good snapshot survives for a later attempt and the
		// failure is stored beside it rather than in place of it.
		entry.Failure = &pl
		entry.Limits = preservedKiloSnapshot(previous, credential.Identity)
	}
	saveKiloCache(entry)
	return pl
}

// preservedKiloSnapshot is the last good reading for this same identity, and
// nothing else: an entry measured under a different login or a different
// selected organization must never carry its numbers into this one.
func preservedKiloSnapshot(previous kiloCacheEntry, identity string) *ProviderLimits {
	if previous.AccountID != identity || previous.Limits == nil {
		return nil
	}
	return previous.Limits
}

func kiloOutcomeFor(outcome kiloPassOutcome) kiloCacheOutcome {
	if outcome == kiloPassFailed {
		return kiloOutcomeFailed
	}
	return kiloOutcomeFetched
}

func kiloProviderLimits(providerID, label string, q kiloPassQuery, nowMs int64) (ProviderLimits, kiloPassOutcome) {
	pl := ProviderLimits{
		ProviderID:  providerID,
		Label:       label,
		Source:      "kilo pass",
		FetchedAtMs: nowMs,
	}

	// A Kilo Pass belongs to the signed-in person; the CLI shows it for personal
	// scope only. A login scoped to an organization therefore has no monthly
	// allowance to meter, and putting the personal Pass on a team's spend would
	// be a different account's reading entirely. This is an answer, not a
	// failure, so it clears a window this scope had.
	if q.OrganizationID != "" {
		pl.Source = "none"
		pl.Note = kiloAccountNote(q.Balance, q.BalanceErr, strPtr(
			"billed to organization "+q.OrganizationID+" — a Kilo Pass is personal scope only, so this team's spend has no monthly allowance to meter"))
		return pl, kiloPassNoPass
	}

	// Exactly one half of the ratio is a half-reported period, not an account
	// without a plan: the server named the plan but not a ratio this can use,
	// and did not say the plan ended.
	if q.Pass.HasSubscription() && q.Pass.HasAllowanceParts != q.Pass.HasUsageParts {
		pl.Source = "none"
		pl.Note = kiloAccountNote(q.Balance, q.BalanceErr, strPtr(
			"Kilo Pass reported only part of this period's credit allowance, which cannot be turned into a percentage"))
		return pl, kiloPassFailed
	}

	// A rejected status is an answer whatever else the response named: the plan is
	// over, so this clears any window this account had. It is read before the
	// ratio because a cancelled or unpaid plan keeps reporting the amounts it last
	// had, and metering those would leave a window on screen for a plan that is no
	// longer paying.
	if q.Pass.HasSubscription() && q.Pass.Status != "" && !kiloPassLiveStatuses[q.Pass.Status] {
		pl.Source = "none"
		pl.Note = kiloAccountNote(q.Balance, q.BalanceErr,
			strPtr("Kilo Pass is "+q.Pass.Status+" — nothing left to meter"))
		return pl, kiloPassNoPass
	}

	if q.Pass.HasRatio() {
		allowance := q.Pass.BaseCreditsUSD + q.Pass.BonusCreditsUSD
		if window := kiloMonthlyWindow(allowance, q.Pass.UsageUSD, q.Pass.NextBillingAt); window != nil {
			// The contract meters a status Kilo actually reported as live. A
			// response that named this period's amounts but no status has not said
			// the plan is paying for the session, which is a missing fact rather
			// than a rejected plan: it preserves this identity's last good window
			// instead of clearing it.
			if q.Pass.Status == "" {
				pl.Source = "none"
				pl.Note = kiloAccountNote(q.Balance, q.BalanceErr, strPtr(
					"Kilo Pass reported no subscription status, so this period's credit allowance cannot be metered"))
				return pl, kiloPassFailed
			}
			pl.Tertiary = window
			plan := "Kilo Pass · monthly credits"
			pl.PlanType = &plan
			pl.Note = kiloAccountNote(q.Balance, q.BalanceErr, nil)
			return pl, kiloPassAllowance
		}
		// The response named a plan but not a usable ratio. It does not say the
		// account lost its Pass, so it is a failure, not a clearing answer:
		// whatever this account had stays.
		pl.Source = "none"
		pl.Note = kiloAccountNote(q.Balance, q.BalanceErr,
			strPtr("Kilo Pass reported no usable credit allowance for this period"))
		return pl, kiloPassFailed
	}

	switch {
	case q.PassErr != nil:
		// A request that failed says nothing about the account.
		pl.Source = "none"
		pl.Note = kiloAccountNote(q.Balance, q.BalanceErr,
			strPtr("Kilo Pass allowance could not be read: "+q.PassErr.Error()))
		return pl, kiloPassFailed
	default:
		// subscription: null. The account pays from a shared credit balance
		// rather than a plan. A normal state, not a failure, and it clears any
		// window this account had.
		pl.Source = "kilo balance"
		pl.Note = kiloAccountNote(q.Balance, q.BalanceErr, strPtr(
			"no Kilo Pass — this account pays from a shared credit balance, which Kilo reports without a limit, so there is no quota percentage to show"))
		return pl, kiloPassNoPass
	}
}

// kiloMonthlyWindow converts a Kilo Pass period into the one window this
// provider publishes.
//
// The allowance is the period's base credits plus its bonus credits: bonus
// credits are granted into the same period and expire with it, so they are
// allowance rather than a top-up sitting outside the window.
func kiloMonthlyWindow(allowance, usage float64, nextBillingAt string) *LimitWindow {
	if allowance <= 0 || usage < 0 || !isFinite(usage) {
		return nil
	}
	used := usage / allowance * 100
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	window := &LimitWindow{UsedPercentage: used}
	minutes := kiloMonthlyWindowMinutes
	window.WindowMinutes = &minutes
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(nextBillingAt)); err == nil {
		resetsAt := t.Unix()
		window.ResetsAt = &resetsAt
	}
	return window
}

// kiloAccountNote composes the non-window facts worth showing.
//
// Every value here is directly reported. Nothing is turned into a percentage,
// because a non-subscriber has no denominator: the balance is stated and the
// absence of a quota is stated explicitly.
func kiloAccountNote(balance *KiloBalance, balanceErr error, extra *string) *string {
	parts := []string{}
	if balance != nil {
		parts = append(parts, fmt.Sprintf("balance $%.2f", balance.Balance))
		if balance.IsDepleted {
			parts = append(parts, "depleted")
		}
	} else if balanceErr != nil {
		parts = append(parts, "balance unavailable")
	}
	if extra != nil && *extra != "" {
		parts = append(parts, *extra)
	}
	if len(parts) == 0 {
		return nil
	}
	note := strings.Join(parts, " · ")
	return &note
}

// kiloHTTPClient never follows a redirect, so a credential-bearing request
// cannot be replayed to a host this collector did not choose.
func kiloHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// kiloGet performs one credential-bearing request. The selected organization is
// sent as the scope header Kilo's own client sends, because the wallet behind
// the balance endpoint belongs to that team; it is omitted for a personal-scope
// login, which is what Kilo's CLI does.
func kiloGet(client *http.Client, endpoint, access, organizationID string) (any, error) {
	request, err := http.NewRequest(http.MethodGet, kiloAPIURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+access)
	request.Header.Set("Accept", "application/json")
	if organizationID != "" {
		request.Header.Set(kiloOrganizationHeader, organizationID)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("kilo rejected the gateway login (HTTP %d)", response.StatusCode)
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("malformed JSON response")
	}
	return payload, nil
}

// fetchKiloBalance reads the prepaid credit wallet of the selected scope.
func fetchKiloBalance(access, organizationID string) (*KiloBalance, error) {
	payload, err := kiloGet(kiloHTTPClient(), "/api/profile/balance", access, organizationID)
	if err != nil {
		return nil, err
	}
	object, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("balance response was not an object")
	}
	balance, ok := object["balance"].(float64)
	if !ok || !isFinite(balance) || balance < 0 {
		return nil, fmt.Errorf("balance missing")
	}
	depleted, _ := object["isDepleted"].(bool)
	return &KiloBalance{Balance: balance, IsDepleted: depleted}, nil
}

// fetchKiloPassState reads the Kilo Pass allowance.
//
// The allowance is personal scope, so this request carries no organization: the
// caller never meters a Pass for an organization-scoped login, and asking for
// one would only invite reading another scope's plan here. A null subscription
// is not an error: it is the account paying from a shared balance instead of a
// plan. It returns a state with no allowance parts, so the caller reports the
// balance and says why there is no bar.
func fetchKiloPassState(access string) (*KiloPassState, error) {
	endpoint := "/api/trpc/kiloPass.getState?" + url.Values{
		"batch": {"1"},
		"input": {`{"0":null}`},
	}.Encode()
	payload, err := kiloGet(kiloHTTPClient(), endpoint, access, "")
	if err != nil {
		return nil, err
	}
	// The tRPC batch reply is an array whose first item holds result.data,
	// either directly or wrapped in a "json" object. Anything else is a shape
	// this collector does not understand and is an error, not a reading.
	root := payload
	if list, ok := payload.([]any); ok {
		if len(list) == 0 {
			return nil, fmt.Errorf("kiloPass returned an empty batch")
		}
		root = list[0]
	}
	result, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("kiloPass response had no result")
	}
	data, ok := result["result"].(map[string]any)
	if !ok {
		// A tRPC error envelope (for example a revoked login) has no result.
		return nil, fmt.Errorf("kiloPass returned an error envelope")
	}
	inner, ok := data["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("kiloPass response had no data")
	}
	if wrapped, ok := inner["json"].(map[string]any); ok {
		inner = wrapped
	}
	raw, present := inner["subscription"]
	if !present {
		return nil, fmt.Errorf("kiloPass response had no subscription field")
	}
	subscription, ok := raw.(map[string]any)
	if !ok {
		// subscription: null — a shared-balance account.
		return &KiloPassState{}, nil
	}
	state := &KiloPassState{}
	state.BaseCreditsUSD, state.HasAllowanceParts = jsonMoney(subscription["currentPeriodBaseCreditsUsd"])
	state.BonusCreditsUSD, _ = jsonMoney(subscription["currentPeriodBonusCreditsUsd"])
	state.UsageUSD, state.HasUsageParts = jsonMoney(subscription["currentPeriodUsageUsd"])
	state.Status, _ = subscription["status"].(string)
	if billing, ok := subscription["nextBillingAt"].(string); ok && strings.TrimSpace(billing) != "" {
		state.NextBillingAt = billing
	} else if renewal, ok := subscription["nextRenewalAt"].(string); ok && strings.TrimSpace(renewal) != "" {
		state.NextBillingAt = renewal
	}
	return state, nil
}

// jsonMoney reads a USD amount. Kilo sends these as JSON numbers, but the same
// value has been spelled both ways upstream, so a clean numeric string is
// accepted too. A negative, non-finite or unparsable amount counts as absent,
// because a bad amount must drop the window rather than scale it.
func jsonMoney(value any) (float64, bool) {
	var amount float64
	switch v := value.(type) {
	case float64:
		amount = v
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		amount = parsed
	default:
		return 0, false
	}
	if !isFinite(amount) || amount < 0 {
		return 0, false
	}
	return amount, true
}
