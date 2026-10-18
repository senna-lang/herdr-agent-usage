/**
 * Tests for the opt-in Grok agent stdio billing probe. A deterministic fake
 * `grok` executable stands in for the CLI, so no test touches a live binary,
 * real credentials, or the network.
 */
package limits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// grokSuccessBody answers initialize, then x.ai/billing with a 25% meter.
const grokSuccessBody = `printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}'
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"monthlyLimit":{"val":10000},"usage":{"totalUsed":{"val":2500}}}}'`

// writeFakeGrok installs a fake grok executable as GROK_BIN. It records its
// argv in <dir>/args and its stdin in <dir>/stdin before running body, so a
// test can prove whether the probe launched at all.
func writeFakeGrok(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$FAKE_GROK_DIR/args\"\n" +
		"while IFS= read -r line; do printf '%s\\n' \"$line\"; done > \"$FAKE_GROK_DIR/stdin\"\n" +
		body + "\n"
	bin := filepath.Join(dir, "grok")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake grok: %v", err)
	}
	t.Setenv("FAKE_GROK_DIR", dir)
	t.Setenv("GROK_BIN", bin)
	return dir
}

// setGrokRPCGate sets USAGEBAR_ENABLE_GROK_RPC for one test; set=false leaves it unset.
func setGrokRPCGate(t *testing.T, set bool, value string) {
	t.Helper()
	if set {
		t.Setenv(grokRPCEnv, value)
		return
	}
	t.Setenv(grokRPCEnv, "")
	if err := os.Unsetenv(grokRPCEnv); err != nil {
		t.Fatalf("unset %s: %v", grokRPCEnv, err)
	}
}

// grokLaunched reports whether the fake grok executable ran in dir.
func grokLaunched(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "args"))
	return err == nil
}

// grokRPCOptions is the collector's real default RPC path. Only the web fetch
// is pinned; TryBillingRPC stays nil so the gate and the fake decide the probe.
func grokRPCOptions(t *testing.T, authJSON string) CollectGrokLimitsOptions {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(authJSON), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	tier := "SUBSCRIPTION_TIER_SUPER_GROK_HEAVY"
	return CollectGrokLimitsOptions{
		AuthPath:        path,
		FetchPlanTier:   func(string) *string { return &tier },
		FetchWebBilling: func(string) *LimitWindow { return nil },
	}
}

// grokActiveAuthJSON is a live SuperGrok login for the observed account.
func grokActiveAuthJSON() string {
	future := time.UnixMilli(grokNowMs).Add(24 * time.Hour).UTC().Format(time.RFC3339)
	return grokAuthJSON(grokAccountEmail, future)
}

func TestTryGrokBillingRPC_GateDecidesLaunch(t *testing.T) {
	disabled := []struct {
		name  string
		set   bool
		value string
	}{
		{name: "unset"},
		{name: "empty", set: true, value: ""},
		{name: "zero", set: true, value: "0"},
		{name: "word true", set: true, value: "true"},
		{name: "padded one", set: true, value: " 1 "},
	}
	for _, tc := range disabled {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeFakeGrok(t, grokSuccessBody)
			setGrokRPCGate(t, tc.set, tc.value)
			if got := TryGrokBillingRPC(grokNowMs, nil); got != nil {
				t.Fatalf("disabled probe returned limits: %+v", got)
			}
			if grokLaunched(dir) {
				t.Fatal("disabled gate still launched grok")
			}
		})
	}

	t.Run("exact one", func(t *testing.T) {
		dir := writeFakeGrok(t, grokSuccessBody)
		setGrokRPCGate(t, true, "1")
		email := grokAccountEmail
		got := TryGrokBillingRPC(grokNowMs, &email)
		if got == nil || got.Primary == nil || got.Primary.UsedPercentage != 25 {
			t.Fatalf("enabled probe: got %+v, want a 25%% meter", got)
		}
		if got.Source != "grok agent stdio x.ai/billing" {
			t.Fatalf("Source: got %q", got.Source)
		}
		if !grokLaunched(dir) {
			t.Fatal("enabled gate did not launch the fake grok")
		}
		args, err := os.ReadFile(filepath.Join(dir, "args"))
		if err != nil {
			t.Fatalf("read args: %v", err)
		}
		if string(args) != "agent\nstdio\n" {
			t.Fatalf("argv: got %q, want agent stdio", args)
		}
		assertGrokProbeRequests(t, dir)
	})
}

// assertGrokProbeRequests checks the stdin the probe sent: an initialize with
// file and terminal capabilities declined, then x.ai/billing.
func assertGrokProbeRequests(t *testing.T, dir string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "stdin"))
	if err != nil {
		t.Fatalf("read stdin record: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("probe sent %d requests, want 2: %q", len(lines), raw)
	}
	// Capabilities are *bool so an omitted declaration fails the presence check
	// instead of passing as the Go zero value false.
	var initReq struct {
		Method string `json:"method"`
		Params struct {
			ClientCapabilities struct {
				FS struct {
					ReadTextFile  *bool `json:"readTextFile"`
					WriteTextFile *bool `json:"writeTextFile"`
				} `json:"fs"`
				Terminal *bool `json:"terminal"`
			} `json:"clientCapabilities"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &initReq); err != nil {
		t.Fatalf("initialize line: %v", err)
	}
	if initReq.Method != "initialize" {
		t.Fatalf("first request: got %q, want initialize", initReq.Method)
	}
	caps := initReq.Params.ClientCapabilities
	for _, c := range []struct {
		name     string
		declared *bool
	}{
		{"fs.readTextFile", caps.FS.ReadTextFile},
		{"fs.writeTextFile", caps.FS.WriteTextFile},
		{"terminal", caps.Terminal},
	} {
		if c.declared == nil {
			t.Fatalf("initialize omitted %s; the probe must declare it as false", c.name)
		}
		if *c.declared {
			t.Fatalf("probe offered client capability %s", c.name)
		}
	}
	var billingReq struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &billingReq); err != nil {
		t.Fatalf("billing line: %v", err)
	}
	if billingReq.Method != "x.ai/billing" || billingReq.ID != 2 {
		t.Fatalf("second request: got %+v, want x.ai/billing id 2", billingReq)
	}
}

func TestCollectGrokLimits_RPCGateOffBorrowsOrIdentityOnly(t *testing.T) {
	t.Run("same account borrows", func(t *testing.T) {
		useAgentDB(t, grokObservationRows(grokAccountEmail, grokAccountID)...)
		dir := writeFakeGrok(t, grokSuccessBody)
		setGrokRPCGate(t, false, "")
		got := CollectGrokLimits(grokNowMs, grokRPCOptions(t, grokActiveAuthJSON()))
		if got.Source != "omp usage_history" || got.Primary == nil {
			t.Fatalf("Source %q, Primary %v: want the borrowed observation", got.Source, got.Primary)
		}
		if got.FetchedAtMs != grokObservedAtMs {
			t.Fatalf("FetchedAtMs: got %d, want the observation time %d", got.FetchedAtMs, grokObservedAtMs)
		}
		if got.Primary.UsedPercentage != 50 || got.Secondary == nil || got.Secondary.UsedPercentage != 25 {
			t.Fatalf("borrowed windows: got primary %+v, secondary %+v, want 50%% and 25%% used", got.Primary, got.Secondary)
		}
		checkGrokPlan(t, got.PlanType, "Heavy")
		if grokLaunched(dir) {
			t.Fatal("disabled gate still launched grok")
		}
	})

	t.Run("no observation is identity only", func(t *testing.T) {
		useAgentDB(t)
		dir := writeFakeGrok(t, grokSuccessBody)
		setGrokRPCGate(t, false, "")
		got := CollectGrokLimits(grokNowMs, grokRPCOptions(t, grokActiveAuthJSON()))
		if got.Source != "grok auth.json (identity only)" {
			t.Fatalf("Source: got %q", got.Source)
		}
		if got.Note == nil || !containsStr(*got.Note, grokAccountEmail) || !containsStr(*got.Note, "disabled") {
			t.Fatalf("Note: got %v, want the account and a disabled reason", got.Note)
		}
		if got.Primary != nil || got.Secondary != nil || got.Tertiary != nil {
			t.Fatalf("identity-only must carry no windows, got %+v", got)
		}
		if grokLaunched(dir) {
			t.Fatal("disabled gate still launched grok")
		}
	})

	t.Run("foreign account refused", func(t *testing.T) {
		const otherEmail = "someone-else@example.com"
		useAgentDB(t, grokObservationRows(otherEmail, "other-account-id")...)
		dir := writeFakeGrok(t, grokSuccessBody)
		setGrokRPCGate(t, false, "")
		got := CollectGrokLimits(grokNowMs, grokRPCOptions(t, grokActiveAuthJSON()))
		if got.Source == "omp usage_history" || got.Primary != nil {
			t.Fatalf("borrowed another account's windows: Source %q", got.Source)
		}
		if got.Note != nil && containsStr(*got.Note, otherEmail) {
			t.Fatalf("Note must not describe the foreign account, got %v", got.Note)
		}
		if grokLaunched(dir) {
			t.Fatal("disabled gate still launched grok")
		}
	})
}

func TestCollectGrokLimits_RPCUnavailableAndFailedHaveDistinctReasons(t *testing.T) {
	t.Run("unavailable", func(t *testing.T) {
		useAgentDB(t)
		t.Setenv("GROK_BIN", filepath.Join(t.TempDir(), "no-such-grok"))
		setGrokRPCGate(t, true, "1")
		got := CollectGrokLimits(grokNowMs, grokRPCOptions(t, grokActiveAuthJSON()))
		if got.Note == nil || !containsStr(*got.Note, "executable unavailable") {
			t.Fatalf("Note: got %v, want an executable-unavailable reason", got.Note)
		}
		if got.Primary != nil || got.Secondary != nil || got.Tertiary != nil {
			t.Fatalf("unavailable probe must carry no windows, got %+v", got)
		}
	})

	t.Run("failed", func(t *testing.T) {
		useAgentDB(t)
		writeFakeGrok(t, "exit 1")
		setGrokRPCGate(t, true, "1")
		got := CollectGrokLimits(grokNowMs, grokRPCOptions(t, grokActiveAuthJSON()))
		if got.Note == nil || !containsStr(*got.Note, "probe failed") {
			t.Fatalf("Note: got %v, want a probe-failed reason", got.Note)
		}
		if containsStr(*got.Note, "executable") {
			t.Fatalf("a probe that ran must not claim the executable is missing: %v", got.Note)
		}
		if got.Primary != nil || got.Secondary != nil || got.Tertiary != nil {
			t.Fatalf("failed probe must carry no windows, got %+v", got)
		}
	})
}

func TestCollectGrokLimits_EnabledRPCRetainsPlan(t *testing.T) {
	useAgentDB(t)
	writeFakeGrok(t, grokSuccessBody)
	setGrokRPCGate(t, true, "1")
	got := CollectGrokLimits(grokNowMs, grokRPCOptions(t, grokActiveAuthJSON()))
	if got.Source != "grok agent stdio x.ai/billing" || got.Primary == nil || got.Primary.UsedPercentage != 25 {
		t.Fatalf("Source %q, Primary %v: want the 25%% RPC meter", got.Source, got.Primary)
	}
	checkGrokPlan(t, got.PlanType, "Heavy")
}

func TestCollectGrokLimits_WebWinsWithoutLaunchingRPC(t *testing.T) {
	useAgentDB(t)
	dir := writeFakeGrok(t, grokSuccessBody)
	setGrokRPCGate(t, true, "1")
	opts := grokRPCOptions(t, grokActiveAuthJSON())
	opts.FetchWebBilling = func(string) *LimitWindow { return &LimitWindow{UsedPercentage: 10} }
	got := CollectGrokLimits(grokNowMs, opts)
	if got.Primary == nil || got.Primary.UsedPercentage != 10 {
		t.Fatalf("Primary %v: want the web meter", got.Primary)
	}
	if grokLaunched(dir) {
		t.Fatal("a successful web response must not launch the RPC probe")
	}
}
