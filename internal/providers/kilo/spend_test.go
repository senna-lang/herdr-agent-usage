/**
 * Tests for Kilo's backend-scoped spend reads.
 *
 * The property under test: a session that moved between backends has to report
 * each backend's own spend, and only the backend the pane is on now. The session
 * row's denormalised totals cannot answer that — they are lifetime figures for
 * every backend the session ever used — so the fixtures write messages for two
 * backends and check the split.
 */
package kilo

import (
	"fmt"
	"path/filepath"
	"testing"
)

// spentMessage is one assistant message as Kilo writes it: per-turn cost and
// tokens, named to the backend that served it.
func spentMessage(providerID string, input, output int, cost float64) string {
	return fmt.Sprintf(
		`{"role":"assistant","providerID":%q,"modelID":"some/model","cost":%g,`+
			`"tokens":{"input":%d,"output":%d,"reasoning":0,"cache":{"read":0,"write":0}}}`,
		providerID, cost, input, output)
}

// spendSession is one pane session and the assistant messages it recorded.
type spendSession struct {
	id       string
	messages []string
}

// writeSpendStore writes every session into one store, with all of their
// messages created at createdAt so a test can place the turns inside or outside
// the window it reads.
func writeSpendStore(t *testing.T, createdAt int64, sessions ...spendSession) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kilo.db")
	db := openStoreDB(t, dbPath)
	for _, session := range sessions {
		insertSession(t, db, storeSession{id: session.id, directory: "/repo", ageMs: liveSessionAge})
		for i, data := range session.messages {
			insertMessage(t, db, fmt.Sprintf("msg_%s_%c", session.id, 'a'+i), session.id, createdAt, data)
		}
	}
	closeStoreDB(t, db)
	return dbPath
}

func TestPaneSpend_ReportsOneBackendAndIgnoresTheOther(t *testing.T) {
	// A session that spent on the gateway and then on a vendor key has two
	// backends' worth of messages. Asking for one must not return the other:
	// otherwise the pane's "$vendor" line carries the gateway's earlier spend.
	useStore(t, writeSpendStore(t, nowForTest(), spendSession{id: "ses_pay", messages: []string{
		spentMessage("kilo", 9000, 100, 1.25),
		spentMessage("deepseek", 27, 3, 0.5),
	}}))

	gateway := PaneSpend(strPtr("ses_pay"), nil, "kilo", 0, nowForTest())
	if len(gateway) != 1 {
		t.Fatalf("gateway rows = %d, want 1", len(gateway))
	}
	vendor := PaneSpend(strPtr("ses_pay"), nil, "deepseek", 0, nowForTest())
	if len(vendor) != 1 {
		t.Fatalf("vendor rows = %d, want 1", len(vendor))
	}
	if vendor[0].Data == gateway[0].Data {
		t.Fatal("both backends returned the same message")
	}
}

func TestPaneSpend_NamesTheSessionsOwnSession(t *testing.T) {
	// Pane spend has to come from the same session the pane's context and billing
	// mode do, or the sidebar sums one session while showing another's. A stale id
	// recovers through the cwd exactly as context resolution does.
	useStore(t, writeSpendStore(t, nowForTest(), spendSession{
		id: "ses_pay", messages: []string{spentMessage("deepseek", 5, 0, 0.25)},
	}))

	if rows := PaneSpend(strPtr("ses_gone"), strPtr("/repo"), "deepseek", 0, nowForTest()); len(rows) != 1 {
		t.Fatalf("recovered pane rows = %d, want 1", len(rows))
	}
	// Nothing to recover from means no spend rather than another pane's.
	if rows := PaneSpend(strPtr("ses_gone"), nil, "deepseek", 0, nowForTest()); rows != nil {
		t.Fatalf("an unattributable pane reported spend: %d rows", len(rows))
	}
}

func TestPaneSpend_RespectsTheWindowAndTheBackend(t *testing.T) {
	turn := nowForTest() - 60_000
	useStore(t, writeSpendStore(t, turn, spendSession{id: "ses_pay", messages: []string{
		spentMessage("deepseek", 10, 0, 0.1),
		spentMessage("openrouter", 20, 0, 0.2),
	}}))

	// A window that ends before the turn is empty, not the session's history.
	if rows := PaneSpend(strPtr("ses_pay"), nil, "deepseek", 0, turn-1); rows != nil {
		t.Fatalf("a window past the last turn reported rows: %d", len(rows))
	}
	if len(PaneSpend(strPtr("ses_pay"), nil, "deepseek", turn-1_000, turn)) != 1 {
		t.Fatal("a window covering the turn reported no rows")
	}
	// A backend the session never used reports nothing rather than everything.
	if rows := PaneSpend(strPtr("ses_pay"), nil, "anthropic", 0, nowForTest()); rows != nil {
		t.Fatalf("an unused backend reported rows: %d", len(rows))
	}
	if rows := PaneSpend(strPtr("ses_pay"), nil, "", 0, nowForTest()); rows != nil {
		t.Fatalf("an unnamed backend reported rows: %d", len(rows))
	}
}

func TestSpendByBackend_GroupsEveryBackendInTheWindow(t *testing.T) {
	// The panel block is account-wide, so it sums every session's spend per
	// backend rather than only the open pane's.
	turn := nowForTest() - 60_000
	useStore(t, writeSpendStore(t, turn,
		spendSession{id: "ses_one", messages: []string{
			spentMessage("deepseek", 30, 1, 0.3),
			spentMessage("kilo", 40, 2, 0.4),
		}},
		spendSession{id: "ses_two", messages: []string{spentMessage("deepseek", 50, 3, 0.5)}},
	))

	byBackend := SpendByBackend(turn-60_000, nowForTest())
	if len(byBackend["deepseek"]) != 2 {
		t.Fatalf("deepseek rows = %d, want both sessions", len(byBackend["deepseek"]))
	}
	if len(byBackend["kilo"]) != 1 {
		t.Fatalf("gateway rows = %d, want 1", len(byBackend["kilo"]))
	}
	// The window bounds both ends: a range past every session is not the account's
	// whole history, and neither is a range that ends before the sessions were
	// last written to.
	if SpendByBackend(turn+60_000, turn+2*60_000) != nil {
		t.Fatal("a window past every session reported spend")
	}
	if SpendByBackend(0, turn-1) != nil {
		t.Fatal("a window before every session reported spend")
	}
}

func TestSpendReadsYieldNothingWithoutAStore(t *testing.T) {
	t.Setenv("KILO_DB", "")
	t.Setenv("KILO_DATA_DIR", t.TempDir())
	now := nowForTest()

	if rows := PaneSpend(strPtr("ses_pay"), strPtr("/repo"), "deepseek", 0, now); rows != nil {
		t.Fatalf("an absent store reported pane spend: %d rows", len(rows))
	}
	if byBackend := SpendByBackend(0, now); byBackend != nil {
		t.Fatalf("an absent store reported account spend: %+v", byBackend)
	}
}
