/**
 * Grok (xAI SuperGrok) limit collection — pure auth/billing parse + identity fallback.
 * Network web billing / agent stdio RPC can be injected later.
 */
package limits

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/planlabels"
)

// GrokAuthEntry is one auth.json entry.
type GrokAuthEntry struct {
	Email     *string
	AuthMode  *string
	TeamID    *string
	ExpiresAt *string
	Key       *string
}

// ParseGrokAuthJSON prefers SuperGrok (auth.x.ai).
func ParseGrokAuthJSON(raw string) *GrokAuthEntry {
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil
	}
	if len(obj) == 0 {
		return nil
	}
	var preferredKey string
	var preferredVal any
	for k, v := range obj {
		if strings.Contains(k, "auth.x.ai") {
			preferredKey, preferredVal = k, v
			break
		}
	}
	if preferredKey == "" {
		for k, v := range obj {
			if strings.Contains(k, "accounts.x.ai") {
				preferredKey, preferredVal = k, v
				break
			}
		}
	}
	if preferredKey == "" {
		for _, v := range obj {
			preferredVal = v
			break
		}
	}
	vm, ok := preferredVal.(map[string]any)
	if !ok {
		return nil
	}
	entry := &GrokAuthEntry{}
	if s, ok := vm["email"].(string); ok {
		entry.Email = &s
	}
	if s, ok := vm["auth_mode"].(string); ok {
		entry.AuthMode = &s
	}
	if s, ok := vm["team_id"].(string); ok {
		entry.TeamID = &s
	}
	if s, ok := vm["expires_at"].(string); ok {
		entry.ExpiresAt = &s
	}
	if s, ok := vm["key"].(string); ok {
		entry.Key = &s
	}
	return entry
}

// ResolveGrokAuthPath returns $GROK_HOME/auth.json or ~/.grok/auth.json.
func ResolveGrokAuthPath() string {
	if home := os.Getenv("GROK_HOME"); home != "" {
		return filepath.Join(home, "auth.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".grok", "auth.json")
}

// ProviderLimitsFromBillingResult builds a window from x.ai/billing JSON-RPC result (cents).
func ProviderLimitsFromBillingResult(result any, nowMs int64, email *string) *ProviderLimits {
	r, ok := result.(map[string]any)
	if !ok {
		return nil
	}
	limitCents := nestedVal(r, "monthlyLimit", "val")
	usedCents := nestedVal(r, "usage", "totalUsed", "val")
	if usedCents == nil {
		usedCents = nestedVal(r, "usage", "includedUsed", "val")
	}
	if limitCents == nil || *limitCents <= 0 || usedCents == nil {
		return nil
	}
	usedPercentage := math.Min(100, math.Max(0, (*usedCents / *limitCents)*100))
	wm := 30 * 24 * 60
	primary := &LimitWindow{UsedPercentage: usedPercentage, WindowMinutes: &wm}
	if cycle, ok := r["billingCycle"].(map[string]any); ok {
		if end, ok := cycle["billingPeriodEnd"].(string); ok && end != "" {
			if sec := parseResetsAtEpochSeconds(end); sec != nil {
				primary.ResetsAt = sec
			}
		}
	}
	note := fmt.Sprintf("used $%.2f / $%.2f", *usedCents/100, *limitCents/100)
	if email != nil {
		note = "account " + *email + " · " + note
	}
	return &ProviderLimits{
		ProviderID:  "grok",
		Label:       "Grok",
		Primary:     primary,
		Source:      "grok agent stdio x.ai/billing",
		FetchedAtMs: nowMs,
		Note:        &note,
	}
}

func nestedVal(m map[string]any, keys ...string) *float64 {
	cur := any(m)
	for _, k := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[k]
	}
	switch v := cur.(type) {
	case float64:
		return &v
	case int:
		f := float64(v)
		return &f
	case int64:
		f := float64(v)
		return &f
	default:
		return nil
	}
}

// CollectGrokLimitsOptions injects optional network helpers for tests.
type CollectGrokLimitsOptions struct {
	AuthPath        string
	FetchWebBilling func(key string) *LimitWindow // returns primary window from web
	FetchPlanTier   func(key string) *string
	// TryBillingRPC overrides the default grok agent stdio x.ai/billing probe.
	TryBillingRPC func(nowMs int64, email *string) *ProviderLimits
}

// TryGrokBillingRPC runs `grok agent stdio` x.ai/billing (often unavailable).
func TryGrokBillingRPC(nowMs int64, email *string) *ProviderLimits {
	grokBin := os.Getenv("GROK_BIN")
	if grokBin == "" {
		grokBin = "grok"
	}
	initMsg := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "1",
			"clientCapabilities": map[string]any{
				"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
				"terminal": false,
			},
		},
	}
	billingMsg := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "x.ai/billing",
		"params":  map[string]any{},
	}
	initB, _ := json.Marshal(initMsg)
	billB, _ := json.Marshal(billingMsg)
	input := string(initB) + "\n" + string(billB) + "\n"

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, grokBin, "agent", "stdio")
	cmd.Stdin = bytes.NewReader([]byte(input))
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil && stdout.Len() == 0 {
		return nil
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg struct {
			ID     *int           `json:"id"`
			Result map[string]any `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.ID != nil && *msg.ID == 2 && msg.Result != nil {
			return ProviderLimitsFromBillingResult(msg.Result, nowMs, email)
		}
	}
	return nil
}

// CollectGrokLimits reads auth and applies identity / web billing.
// When opts fetchers are nil, production HTTP implementations are used.
func CollectGrokLimits(nowMs int64, opts CollectGrokLimitsOptions) ProviderLimits {
	path := opts.AuthPath
	if path == "" {
		path = ResolveGrokAuthPath()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		note := "no ~/.grok/auth.json — run `grok login`"
		return ProviderLimits{ProviderID: "grok", Label: "Grok", Source: "none", FetchedAtMs: nowMs, Note: &note}
	}
	auth := ParseGrokAuthJSON(string(raw))
	if auth == nil {
		note := "no ~/.grok/auth.json — run `grok login`"
		return ProviderLimits{ProviderID: "grok", Label: "Grok", Source: "none", FetchedAtMs: nowMs, Note: &note}
	}

	if auth.ExpiresAt != nil {
		if t, err := time.Parse(time.RFC3339, *auth.ExpiresAt); err == nil && t.UnixMilli() < nowMs {
			note := "token expired"
			if auth.Email != nil {
				note = fmt.Sprintf("token expired (%s) — run `grok login`", *auth.Email)
			} else {
				note = "token expired (unknown) — run `grok login`"
			}
			return ProviderLimits{ProviderID: "grok", Label: "Grok", Source: "grok auth.json", FetchedAtMs: nowMs, Note: &note}
		}
	}

	fetchPlan := opts.FetchPlanTier
	if fetchPlan == nil {
		fetchPlan = func(key string) *string {
			return FetchGrokSubscriptionTier(key, "")
		}
	}
	fetchWeb := opts.FetchWebBilling
	if fetchWeb == nil {
		fetchWeb = func(key string) *LimitWindow {
			snap := FetchGrokWebBilling(key, "")
			if snap == nil {
				return nil
			}
			return LimitWindowFromWebBilling(*snap, nowMs)
		}
	}

	var plan *string
	if auth.Key != nil && *auth.Key != "" {
		if tier := fetchPlan(*auth.Key); tier != nil {
			plan = planlabels.GrokPlanLabel(tier)
		}
	}

	if auth.Key != nil && *auth.Key != "" {
		if primary := fetchWeb(*auth.Key); primary != nil {
			var note *string
			if auth.Email != nil {
				n := "account " + *auth.Email
				note = &n
			}
			return ProviderLimits{
				ProviderID: "grok", Label: "Grok", Primary: primary, PlanType: plan,
				Source: "grok.com GetGrokCreditsConfig", FetchedAtMs: nowMs, Note: note,
			}
		}
	}

	tryRPC := opts.TryBillingRPC
	if tryRPC == nil {
		tryRPC = TryGrokBillingRPC
	}
	if fromRPC := tryRPC(nowMs, auth.Email); fromRPC != nil {
		fromRPC.PlanType = plan
		return *fromRPC
	}

	email := "signed in"
	if auth.Email != nil {
		email = *auth.Email
	}
	note := email + " · rate meters unavailable (web billing failed; x.ai/billing not on agent stdio)"
	return ProviderLimits{
		ProviderID: "grok", Label: "Grok", PlanType: plan,
		Source: "grok auth.json (identity only)", FetchedAtMs: nowMs, Note: &note,
	}
}
