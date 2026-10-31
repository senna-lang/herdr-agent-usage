/**
 * Devin CLI limit collection.
 *
 * Devin publishes no local quota ledger: allowance lives behind the
 * seat-management endpoint the CLI's own login talks to, so this collector
 * reuses that credential — the windsurf_api_key in Devin's credentials.toml —
 * for the same request Devin's own tooling makes. The key is sent only to
 * the api_server_url the login recorded (https only, no redirects) and is
 * never written to disk by this plugin.
 *
 * Devin meters against percentage-of-plan quota windows (daily and weekly
 * for current plans), not token counts: the API returns the share each
 * window has LEFT, which the collector inverts into the displayed "used"
 * bar. Overage spend is a dollar balance with no window, so it is reported
 * as a note, never as a percentage — the same rule the other collectors
 * apply to facts that have no denominator.
 */
package limits

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/devin"
)

// devinDailyWindowMinutes / devinWeeklyWindowMinutes are display hints for
// the window labels; the countdown is driven by the reset timestamps the
// API reports, never by these constants.
const (
	devinDailyWindowMinutes  = 24 * 60
	devinWeeklyWindowMinutes = 7 * 24 * 60
)

// devinCompatVersion is the client version reported to the endpoint. Devin's
// seat service only checks that a plausible Windsurf-family client is
// asking, so the version OpenUsage converged on is reused.
const devinCompatVersion = "1.108.2"

// DevinPlanStatus is the account's quota snapshot, already normalized out
// of the seat-management response.
type DevinPlanStatus struct {
	PlanName               string
	HideDailyQuota         bool
	DailyRemainingPercent  *float64
	DailyResetsAt          *int64 // unix seconds
	WeeklyRemainingPercent *float64
	WeeklyResetsAt         *int64 // unix seconds
	// OverageBalanceMicros is the extra-usage wallet in USD millionths.
	// Negative means the account owes money.
	OverageBalanceMicros int64
	// HasOverageBalance distinguishes an absent field from a real zero:
	// proto3 JSON drops zero-valued fields, and a plan without the wallet
	// reports nothing at all.
	HasOverageBalance bool
}

// CollectDevinLimitsOptions injects every dependency, so no test reaches
// the network or the developer's real Devin account.
type CollectDevinLimitsOptions struct {
	// CredentialsPath defaults to Devin CLI's resolved credentials.toml.
	CredentialsPath string
	// FetchStatus defaults to the real endpoint.
	FetchStatus func(serverURL, apiKey string) (*DevinPlanStatus, error)
	// Now defaults to the passed nowMs; tests pin it.
	Now func() int64
}

// devinQuotaOutcome is what one fetch actually established.
type devinQuotaOutcome int

const (
	// devinQuotaAnswer: the endpoint responded — with windows, or with a
	// readable "nothing to meter" account. Either way it is a fact.
	devinQuotaAnswer devinQuotaOutcome = iota
	// devinQuotaFailed: the request, the decode, or the shape failed. Devin
	// said nothing about the account, so nothing is cleared.
	devinQuotaFailed
)

// CollectDevinLimits builds a ProviderLimits for the signed-in Devin
// account. The result is cached against the login's hashed identity, so a
// different account never sees numbers its predecessor produced, and a
// failed fetch preserves the last good reading instead of replacing it
// with the failure's.
func CollectDevinLimits(nowMs int64, opts CollectDevinLimitsOptions) ProviderLimits {
	const providerID = "devin"
	const label = "Devin"

	credentialsPath := opts.CredentialsPath
	if credentialsPath == "" {
		credentialsPath = devin.ResolveDevinCredentialsPath()
	}
	credential := devin.LoadCredentials(credentialsPath)
	if credential == nil {
		pl := ProviderLimits{ProviderID: providerID, Label: label, Source: "none", FetchedAtMs: nowMs}
		pl.Note = strPtr("no Devin CLI login in credentials.toml — run `devin login`")
		return pl
	}

	accountID := devin.CredentialID(credential.APIKey)
	previous, _ := readDevinCache()
	if cached, ok := cachedDevinLimits(previous, accountID, nowMs); ok {
		return cached
	}

	fetchStatus := opts.FetchStatus
	if fetchStatus == nil {
		fetchStatus = fetchDevinUserStatus
	}

	status, err := fetchStatus(credential.APIServerURL, credential.APIKey)
	pl, outcome := devinProviderLimits(providerID, label, status, err, nowMs)

	entry := devinCacheEntry{
		FetchedAtMs: nowMs,
		AccountID:   accountID,
		Outcome:     devinOutcomeFetched,
		Limits:      &pl,
	}
	if outcome == devinQuotaFailed {
		// The attempt is recorded in its own right; the reading is not. A blip
		// at the endpoint says nothing about the account's quota, so the last
		// good snapshot survives for a later attempt and the failure is stored
		// beside it rather than in place of it.
		entry.Outcome = devinOutcomeFailed
		entry.Failure = &pl
		entry.Limits = preservedDevinSnapshot(previous, accountID)
	}
	saveDevinCache(entry)
	return pl
}

// preservedDevinSnapshot is the last good reading for this same identity,
// and nothing else: an entry measured under a different login must never
// carry its numbers into this one.
func preservedDevinSnapshot(previous devinCacheEntry, accountID string) *ProviderLimits {
	if previous.AccountID != accountID || previous.Limits == nil {
		return nil
	}
	return previous.Limits
}

func devinProviderLimits(providerID, label string, status *DevinPlanStatus, err error, nowMs int64) (ProviderLimits, devinQuotaOutcome) {
	pl := ProviderLimits{
		ProviderID:  providerID,
		Label:       label,
		Source:      "devin api",
		FetchedAtMs: nowMs,
	}
	if err != nil {
		pl.Source = "none"
		pl.Note = strPtr("Devin quota could not be read: " + err.Error())
		return pl, devinQuotaFailed
	}

	daily := devinQuotaWindow(status.DailyRemainingPercent, status.DailyResetsAt, devinDailyWindowMinutes)
	weekly := devinQuotaWindow(status.WeeklyRemainingPercent, status.WeeklyResetsAt, devinWeeklyWindowMinutes)

	// A plan that hides its daily window still meters it: the Weekly slot
	// carries the daily share when no weekly window exists, because a metered
	// window is better shown under the other label than not shown at all.
	if status.HideDailyQuota {
		if weekly != nil {
			pl.Secondary = weekly
		} else {
			pl.Secondary = daily
		}
	} else {
		pl.Primary = daily
		pl.Secondary = weekly
	}

	if pl.Primary == nil && pl.Secondary == nil {
		pl.Source = "none"
		pl.Note = strPtr("Devin reports no quota for this account")
		return pl, devinQuotaAnswer
	}
	if status.PlanName != "" {
		pl.PlanType = &status.PlanName
	}
	pl.Note = devinAccountNote(status)
	return pl, devinQuotaAnswer
}

// devinQuotaWindow converts one reported window. Devin reports the share
// LEFT; the display inverts it. proto3 JSON omits zero-valued fields, so a
// window whose reset is present but whose share is absent has nothing left.
func devinQuotaWindow(remaining *float64, resetsAt *int64, windowMinutes int) *LimitWindow {
	if remaining == nil && resetsAt == nil {
		return nil
	}
	left := 0.0
	if remaining != nil {
		left = *remaining
	}
	used := 100 - left
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	w := &LimitWindow{UsedPercentage: used, WindowMinutes: &windowMinutes}
	if resetsAt != nil && *resetsAt > 0 {
		w.ResetsAt = resetsAt
	}
	return w
}

// devinAccountNote composes the non-window facts worth showing. The overage
// wallet is a dollar balance, not a quota, so it is stated rather than
// metered; it is omitted entirely when the plan does not carry one.
func devinAccountNote(status *DevinPlanStatus) *string {
	if !status.HasOverageBalance {
		return nil
	}
	usd := float64(status.OverageBalanceMicros) / 1_000_000
	if usd == 0 {
		return nil
	}
	note := fmt.Sprintf("extra usage balance %s", formatDevinUSD(usd))
	return &note
}

// formatDevinUSD renders a signed micro-dollar balance with the sign in
// front of the currency symbol ("-$0.32"), matching how a negative wallet
// reads everywhere else money is displayed.
func formatDevinUSD(usd float64) string {
	if usd < 0 {
		return fmt.Sprintf("-$%.2f", -usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// devinHTTPClient never follows a redirect, so a credential-bearing request
// cannot be replayed to a host this collector did not choose.
func devinHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// fetchDevinUserStatus performs the one request Devin's own tooling makes
// for quota. The Connect-protocol call carries the API key inside a
// metadata envelope, not an Authorization header, matching the shape
// Windsurf-family clients send.
func fetchDevinUserStatus(serverURL, apiKey string) (*DevinPlanStatus, error) {
	if u, err := url.Parse(serverURL); err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("credentials.toml names an unsafe api_server_url")
	}
	body, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"apiKey":           apiKey,
			"ideName":          "devin",
			"ideVersion":       devinCompatVersion,
			"extensionName":    "devin",
			"extensionVersion": devinCompatVersion,
			"locale":           "en",
		},
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodPost,
		strings.TrimRight(serverURL, "/")+"/exa.seat_management_pb.SeatManagementService/GetUserStatus",
		bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	response, err := devinHTTPClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("Devin rejected the CLI credential (HTTP %d)", response.StatusCode)
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return parseDevinUserStatus(payload)
}

// parseDevinUserStatus extracts the quota facts from the seat-management
// response. proto3 JSON spells int64 fields as strings, so every integer
// accepts either a JSON string or a JSON number.
func parseDevinUserStatus(body []byte) (*DevinPlanStatus, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("malformed JSON response")
	}
	userStatus, ok := root["userStatus"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("userStatus missing from response")
	}
	planStatus, _ := userStatus["planStatus"].(map[string]any)
	planInfo, _ := planStatus["planInfo"].(map[string]any)

	status := &DevinPlanStatus{}
	if name, ok := planInfo["planName"].(string); ok {
		status.PlanName = strings.TrimSpace(name)
	}
	if hide, ok := planInfo["hideDailyQuota"].(bool); ok {
		status.HideDailyQuota = hide
	}
	status.DailyRemainingPercent = devinPercent(planStatus["dailyQuotaRemainingPercent"])
	status.WeeklyRemainingPercent = devinPercent(planStatus["weeklyQuotaRemainingPercent"])
	status.DailyResetsAt = devinInt64Ptr(planStatus["dailyQuotaResetAtUnix"])
	status.WeeklyResetsAt = devinInt64Ptr(planStatus["weeklyQuotaResetAtUnix"])
	if micros, ok := devinInt64(planStatus["overageBalanceMicros"]); ok {
		status.OverageBalanceMicros = micros
		status.HasOverageBalance = true
	}
	return status, nil
}

// devinPercent reads a quota share. Out-of-range shares are kept and
// clamped by devinQuotaWindow — an over-consumed window can report a
// negative remainder — while an unreadable share counts as absent: a
// schema change must drop the window, not fabricate a percentage.
func devinPercent(v any) *float64 {
	switch n := v.(type) {
	case float64:
		if isFinite(n) {
			return &n
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil && isFinite(f) {
			return &f
		}
	}
	return nil
}

// devinInt64 reads a proto3 int64 spelled as a JSON string or number.
func devinInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case string:
		if i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
			return i, true
		}
	}
	return 0, false
}

func devinInt64Ptr(v any) *int64 {
	if n, ok := devinInt64(v); ok {
		return &n
	}
	return nil
}
