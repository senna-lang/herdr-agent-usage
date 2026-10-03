/**
 * Tests for Kilo pane routing.
 *
 * Kilo drives several providers from one CLI. The pane is a Kilo pane; the
 * backend recorded on its session is what decides whose allowance it spends,
 * and these tests pin that distinction for the two cases that matter: a session
 * on the Kilo Gateway, and a session on a backend with its own quota.
 */
package limits

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/kilo"
)

// kiloSessionSchema is the slice of Kilo's session store these tests read: the
// pane's session row, its messages and its parts. parent_id is what tells a
// subagent child session apart from a top-level pane session.
const kiloSessionSchema = `
	CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT, parent_id TEXT,
		time_updated INTEGER DEFAULT 0, time_archived INTEGER, cost REAL DEFAULT 0 NOT NULL,
		tokens_input INTEGER DEFAULT 0 NOT NULL, tokens_output INTEGER DEFAULT 0 NOT NULL,
		tokens_reasoning INTEGER DEFAULT 0 NOT NULL, tokens_cache_read INTEGER DEFAULT 0 NOT NULL,
		tokens_cache_write INTEGER DEFAULT 0 NOT NULL, model TEXT);
	CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT,
		time_created INTEGER, data TEXT);
	CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT,
		time_created INTEGER, data TEXT);`

// seedKiloPane writes a Kilo store with one session served by backend, and an
// auth.json describing that backend's credential.
func seedKiloPane(t *testing.T, sessionID, backend string, auth string) OpenPaneSnapshot {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kilo.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mustExecKilo(t, db, kiloSessionSchema)
	mustExecKilo(t, db, `INSERT INTO session (id, directory, time_updated) VALUES (?, '/repo', ?)`,
		sessionID, time.Now().UnixMilli())
	_, err = db.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?,?,?,?)`,
		"msg_a", sessionID, 1,
		`{"role":"assistant","providerID":"`+backend+`","modelID":"some/model","tokens":{"input":10,"output":2}}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KILO_DB", dbPath)
	t.Setenv("KILO_DATA_DIR", dir)
	t.Setenv("KILO_MODELS_PATH", filepath.Join(dir, "absent.json"))
	kilo.ClearModelsCatalogCache()
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(auth), 0o600); err != nil {
		t.Fatal(err)
	}
	return OpenPaneSnapshot{PaneID: "w1:p1", Agent: "kilo", Label: "kilo", SessionID: &sessionID}
}

func mustExecKilo(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestKiloPane_OnTheGatewayOwnsNoPayAsYouGoBackend(t *testing.T) {
	// The Kilo Gateway is a Kilo allowance. Naming it as a pay-as-you-go
	// backend would send the pane to a burn-total block that says nothing about
	// its actual billing.
	pane := seedKiloPane(t, "ses_gw", "kilo",
		`{"kilo":{"type":"oauth","access":"tok"},"opencode-go":{"type":"api","key":"og"}}`)
	if got := kiloPaneBackendID(pane); got != "kilo" {
		t.Fatalf("backend = %q", got)
	}
	if got := payAsYouGoBackendID("kilo", pane); got != "" {
		t.Fatalf("gateway reported as pay-as-you-go: %q", got)
	}
	if route, ok := paneSubscriptionRoute("kilo", pane); ok {
		t.Fatalf("gateway resolved to a foreign subscription route: %+v", route)
	}
}

func TestKiloPane_OnOpenCodeGoRoutesToThatAccountsCollector(t *testing.T) {
	// This is the case the panel exists for: a Kilo pane spending an OpenCode
	// Go login must show that account's windows, under its own name, while the
	// harness stays Kilo.
	pane := seedKiloPane(t, "ses_og", "opencode-go",
		`{"kilo":{"type":"oauth","access":"tok"},"opencode-go":{"type":"api","key":"og"}}`)
	if got := kiloPaneBackendID(pane); got != "opencode-go" {
		t.Fatalf("backend = %q", got)
	}
	route, ok := paneSubscriptionRoute("kilo", pane)
	if !ok {
		t.Fatal("opencode-go backend did not resolve to a subscription route")
	}
	if route.CollectorProviderID != "opencode" || route.DisplayProviderID != "opencode-go" {
		t.Fatalf("route = %+v", route)
	}
	if got := SubscriptionLimitsProviderID("kilo", pane); got != "opencode" {
		t.Fatalf("collector provider = %q", got)
	}
	if got := SubscriptionDisplayProviderID("kilo", pane); got != "opencode-go" {
		t.Fatalf("display provider = %q", got)
	}
	if got := payAsYouGoBackendID("kilo", pane); got != "opencode-go" {
		t.Fatalf("pay-as-you-go backend = %q", got)
	}
}

func TestKiloPane_GatewaySessionIsBillableAndNotHidden(t *testing.T) {
	pane := seedKiloPane(t, "ses_gw", "kilo", `{"kilo":{"type":"oauth","access":"tok"}}`)
	mode := paneBillingModeWith(nil, nil, nil, nil, "kilo", pane)
	if mode == BillingPayAsYouGo {
		t.Fatal("a Kilo Gateway session was classified pay-as-you-go and hidden")
	}
}

func TestKiloPane_ForeignBackendWithAnApiKeyIsPayAsYouGo(t *testing.T) {
	// An API-key backend spends per token, so the pane is not drawing on a
	// Kilo allowance and must not be shown one.
	pane := seedKiloPane(t, "ses_og", "opencode-go",
		`{"kilo":{"type":"oauth","access":"tok"},"opencode-go":{"type":"api","key":"og"}}`)
	route, ok := paneSubscriptionRoute("kilo", pane)
	if ok && route.DisplayProviderID == "kilo" {
		t.Fatal("route pointed back at Kilo itself")
	}
}

func TestKiloPane_NoBackendEvidenceFailsOpen(t *testing.T) {
	// A session that has not recorded a backend yet must stay visible rather
	// than be hidden as pay-as-you-go on no evidence.
	pane := seedKiloPane(t, "ses_empty", "kilo", `{"kilo":{"type":"oauth","access":"tok"}}`)
	if got := kiloPaneBackendID(pane); got == "" {
		if mode := paneBillingModeWith(nil, nil, nil, nil, "kilo", pane); mode == BillingPayAsYouGo {
			t.Fatal("no evidence classified pay-as-you-go")
		}
	}
}

// seedKiloPaneSpend writes a Kilo store whose single session recorded assistant
// turns against each of the given backends, plus the auth.json describing the
// credential. A nil model writes SQL NULL, as it does for any session Kilo never
// recorded a model for.
//
// Spend is read from the messages rather than the session row's denormalised
// counters, because those mix every backend the session ever used — and a
// session that switched backends is exactly the case a backend-scoped read has
// to get right.
func seedKiloPaneSpend(t *testing.T, backend string, messages []string, model any, auth string) OpenPaneSnapshot {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kilo.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mustExecKilo(t, db, kiloSessionSchema)
	mustExecKilo(t, db, `INSERT INTO session (id, directory, time_updated, model) VALUES ('ses_pay', '/repo', ?, ?)`,
		time.Now().UnixMilli(), model)
	for i, data := range messages {
		_, err := db.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?,?,?,?)`,
			"msg_"+string(rune('a'+i)), "ses_pay", paneClock()-60_000+int64(i), data)
		if err != nil {
			t.Fatal(err)
		}
	}
	// The newest assistant turn is what names the pane's live backend, so it is
	// written last and carries the newest time.
	if _, err := db.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?,?,?,?)`,
		"msg_live", "ses_pay", paneClock()-30_000,
		`{"role":"assistant","providerID":"`+backend+`","modelID":"some/model","cost":0,`+
			`"tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}}}`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	installKiloStore(t, dir, dbPath, auth)
	sessionID := "ses_pay"
	return OpenPaneSnapshot{
		PaneID: "w1:p1", Agent: "kilo", Label: "kilo",
		SessionID: &sessionID, Cwd: strPtrForPane("/repo"),
	}
}

// seedKiloSharedDirectory writes one Kilo store holding a pane session per given
// id, all in the same directory, which is what makes that directory ambiguous:
// two live panes in one repository share a cwd.
func seedKiloSharedDirectory(t *testing.T, backend string, ids []string, auth string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kilo.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mustExecKilo(t, db, kiloSessionSchema)
	for i, id := range ids {
		mustExecKilo(t, db, `INSERT INTO session (id, directory, time_updated) VALUES (?, '/repo', ?)`,
			id, time.Now().UnixMilli())
		mustExecKilo(t, db, `INSERT INTO message (id, session_id, time_created, data) VALUES (?,?,?,?)`,
			"msg_"+id, id, paneClock()-60_000+int64(i),
			`{"role":"assistant","providerID":"`+backend+`","modelID":"some/model","cost":0.5,`+
				`"tokens":{"input":27,"output":0,"reasoning":0,"cache":{"read":0,"write":0}}}`)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	installKiloStore(t, dir, dbPath, auth)
}

// installKiloStore points every Kilo path resolver at one fixture store, and
// writes the credential store beside it.
func installKiloStore(t *testing.T, dir, dbPath, auth string) {
	t.Helper()
	t.Setenv("KILO_DB", dbPath)
	t.Setenv("KILO_DATA_DIR", dir)
	t.Setenv("KILO_MODELS_PATH", filepath.Join(dir, "absent.json"))
	kilo.ClearModelsCatalogCache()
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(auth), 0o600); err != nil {
		t.Fatal(err)
	}
}

// paneClock is the wall clock the Kilo pane fixtures are written and read
// against, unlike the pinned nowMs the limit-collection tests use for cache-TTL
// arithmetic.
//
// The pane side needs it: session.time_updated has to look recent for the cwd
// fallback, which compares it with the real clock, and the recorded turns have to
// fall inside the panel's rolling windows. A pane fixture pinned to a fixed
// instant cannot satisfy both.
func paneClock() int64 { return time.Now().UnixMilli() }

// kiloTurn is one recorded assistant turn: what a backend spent and moved.
func kiloTurn(providerID string, input, output int, cost float64) string {
	return fmt.Sprintf(
		`{"role":"assistant","providerID":%q,"modelID":"some/model","cost":%g,`+
			`"tokens":{"input":%d,"output":%d,"reasoning":0,"cache":{"read":0,"write":0}}}`,
		providerID, cost, input, output)
}

func strPtrForPane(s string) *string { return &s }

func TestKiloPane_PayAsYouGoShowsTheSessionsOwnSpend(t *testing.T) {
	// A backend Kilo holds its own key for is not a Kilo allowance, so the pane
	// is classified pay-as-you-go and the sidebar then asks for its spend. Before
	// this was wired, that block fell through to a dispatch with no Kilo case and
	// the pane showed its backend beside an empty total.
	pane := seedKiloPaneSpend(t, "deepseek", []string{kiloTurn("deepseek", 27, 0, 0.5)}, nil,
		`{"kilo":{"type":"oauth","access":"tok"},"deepseek":{"type":"api","key":"dk"}}`)

	mode := paneBillingModeWith(nil, nil, nil, nil, "kilo", pane)
	if mode != BillingPayAsYouGo {
		t.Fatalf("mode = %v, want pay-as-you-go", mode)
	}
	if backend := PaneBackendID("kilo", pane); backend != "deepseek" {
		t.Fatalf("pay-as-you-go backend = %q", backend)
	}
	tokens, costUSD := PaneTotalUsage("kilo", pane, paneClock())
	if tokens != 27 || costUSD != 0.5 {
		t.Fatalf("totals = %v/%v, want 27/0.50", tokens, costUSD)
	}
}

func TestKiloPane_PayAsYouGoSpendIsScopedToTheCurrentBackend(t *testing.T) {
	// A Kilo session can move between backends: the gateway for a while, then a
	// vendor Kilo holds a key for. The sidebar's label is the backend the pane is
	// on now, so the earlier gateway spend must not be counted under it — that
	// spend already sits on the Kilo Pass window, and reading the session totals
	// would show it twice.
	pane := seedKiloPaneSpend(t, "deepseek", []string{
		kiloTurn("kilo", 9000, 100, 1.25),
		kiloTurn("deepseek", 27, 3, 0.5),
	}, nil, `{"kilo":{"type":"oauth","access":"tok"},"deepseek":{"type":"api","key":"dk"}}`)

	if backend := PaneBackendID("kilo", pane); backend != "deepseek" {
		t.Fatalf("pay-as-you-go backend = %q", backend)
	}
	tokens, costUSD := PaneTotalUsage("kilo", pane, paneClock())
	if tokens != 30 || costUSD != 0.5 {
		t.Fatalf("totals = %v/%v, want the current backend's 30/0.50", tokens, costUSD)
	}
}

func TestKiloPane_PayAsYouGoSpendSurvivesANullModelColumn(t *testing.T) {
	// session.model is SQL NULL on any session Kilo never recorded a model for,
	// which is most of them, and a turn can name no model either. Reading either
	// as a string would fail the whole read and drop the pane's spend with it.
	pane := seedKiloPaneSpend(t, "deepseek", []string{
		`{"role":"assistant","providerID":"deepseek","cost":1.25,` +
			`"tokens":{"input":10,"output":20,"reasoning":0,"cache":{"read":30,"write":0}}}`,
	}, nil, `{"kilo":{"type":"oauth","access":"tok"},"deepseek":{"type":"api","key":"dk"}}`)

	tokens, costUSD := PaneTotalUsage("kilo", pane, paneClock())
	if tokens != 60 || costUSD != 1.25 {
		t.Fatalf("totals = %v/%v, want 60/1.25", tokens, costUSD)
	}
}

func TestKiloPane_StaleSessionIDStillFeedsBothContextAndBilling(t *testing.T) {
	// Context resolution already recovers a stale id through the pane's cwd.
	// Billing has to recover it the same way, or the pane displays a Kilo context
	// while its billing mode sees no backend at all — and a foreign API-key
	// session then shows as Unknown beside the Kilo allowance rather than as the
	// pay-as-you-go pane it is.
	pane := seedKiloPaneSpend(t, "deepseek", []string{kiloTurn("deepseek", 27, 0, 0.5)}, nil,
		`{"kilo":{"type":"oauth","access":"tok"},"deepseek":{"type":"api","key":"dk"}}`)
	pane.SessionID = strPtrForPane("ses_gone")

	if backend := kiloPaneBackendID(pane); backend != "deepseek" {
		t.Fatalf("backend = %q, want the recovered session's", backend)
	}
	if mode := paneBillingModeWith(nil, nil, nil, nil, "kilo", pane); mode != BillingPayAsYouGo {
		t.Fatalf("mode = %v, want pay-as-you-go", mode)
	}
	if tokens, costUSD := PaneTotalUsage("kilo", pane, paneClock()); tokens != 27 || costUSD != 0.5 {
		t.Fatalf("totals = %v/%v, want the recovered session's 27/0.50", tokens, costUSD)
	}

	// With no directory to recover from, nothing is attributed — to either.
	orphan := pane
	orphan.Cwd = nil
	if backend := kiloPaneBackendID(orphan); backend != "" {
		t.Fatalf("an unattributable pane named a backend: %q", backend)
	}
	if tokens, costUSD := PaneTotalUsage("kilo", orphan, paneClock()); tokens != 0 || costUSD != 0 {
		t.Fatalf("an unattributable pane reported spend: %v/%v", tokens, costUSD)
	}
}

func TestKiloPane_GatewaySpendIsNeverShownAsPayAsYouGo(t *testing.T) {
	// The gateway is an allowance, not a burn. A pane on it is not classified
	// pay-as-you-go, and asking for a pay-as-you-go total anyway must return
	// nothing rather than re-present the Pass spend as a vendor bill.
	pane := seedKiloPaneSpend(t, "kilo", []string{kiloTurn("kilo", 9000, 100, 1.25)}, nil,
		`{"kilo":{"type":"oauth","access":"tok"}}`)
	if mode := paneBillingModeWith(nil, nil, nil, nil, "kilo", pane); mode == BillingPayAsYouGo {
		t.Fatal("a Kilo Gateway pane was classified pay-as-you-go")
	}
	if tokens, costUSD := PaneTotalUsage("kilo", pane, paneClock()); tokens != 0 || costUSD != 0 {
		t.Fatalf("gateway spend surfaced as pay-as-you-go: %v/%v", tokens, costUSD)
	}
}

func TestKiloCredentialType_ComesFromKilosOwnStoreOnly(t *testing.T) {
	// A Kilo session on OpenCode Go must be classified from Kilo's own auth.json
	// entry for that provider, never from OpenCode's separate store.
	pane := seedKiloPane(t, "ses_og", "opencode-go",
		`{"kilo":{"type":"oauth","access":"tok"},"opencode-go":{"type":"api","key":"og"}}`)
	if got := paneCredentialType("kilo", pane); got != "api" {
		t.Fatalf("credential type = %q", got)
	}
	// And the gateway's own login is reported as oauth, so a Kilo Gateway pane
	// is never mistaken for an API-key session.
	gateway := seedKiloPane(t, "ses_gw2", "kilo", `{"kilo":{"type":"oauth","access":"tok"}}`)
	if got := paneCredentialType("kilo", gateway); got != kilo.CredentialType("kilo") {
		t.Fatalf("gateway credential type = %q", got)
	}
}
