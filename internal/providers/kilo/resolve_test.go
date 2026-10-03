/**
 * Tests for Kilo context resolution against a real-shaped session store.
 *
 * The fixtures mirror the rows Kilo actually writes: step-finish parts carrying
 * per-step context, assistant messages naming the backend, and the denormalised
 * session totals Kilo backfills from its messages.
 */
package kilo

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A step-finish row as Kilo writes it: prompt-cache occupancy for that step,
// with the model block only present on newer rows.
const stepWithModel = `{"reason":"tool-calls","type":"step-finish","time":{"start":1,"end":2,"elapsed":5592},
 "model":{"providerID":"kilo","modelID":"~openai/gpt-mini-latest"},
 "metrics":{"generation":79.04,"source":"computed"},
 "tokens":{"total":30494,"input":1219,"output":442,"reasoning":0,"cache":{"write":0,"read":28833}},
 "cost":0}`

// The same step on an older row, where the model is only on the message.
const stepWithoutModel = `{"reason":"tool-calls","type":"step-finish","time":{"start":1,"end":2,"elapsed":1},
 "tokens":{"total":74588,"input":74566,"output":4,"reasoning":18,"cache":{"write":0,"read":0}},
 "cost":0}`

const assistantMessage = `{"role":"assistant","mode":"general","cost":0,
 "tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}},
 "modelID":"~openai/gpt-mini-latest","providerID":"kilo"}`

// A free-model step: every counter is zero. Reporting a 0-token context would
// present as an untouched window, so this must yield no usage at all.
const costOnlyStep = `{"type":"step-finish","tokens":{"total":0,"input":0,"output":0,"reasoning":0,
 "cache":{"read":0,"write":0}},"cost":0}`

// writeStore builds a Kilo session store with the given session rows.
func writeStore(t *testing.T, steps []string, messages []string, modelJSON any) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kilo.db")
	db := openStoreDB(t, dbPath)
	insertSession(t, db, storeSession{
		id: "ses_test", directory: "/repo", ageMs: liveSessionAge, model: modelJSON,
	})
	for i, data := range messages {
		insertMessage(t, db, "msg_"+string(rune('a'+i)), "ses_test", int64(100-i), data)
	}
	for i, data := range steps {
		insertPart(t, db, "prt_"+string(rune('a'+i)), "msg_a", "ses_test", int64(100-i), data)
	}
	closeStoreDB(t, db)
	return dbPath
}

// storeSession is one session row to write. ageMs is how long ago the session
// was last written to, and parentID names the parent of a subagent child
// session (nil for a top-level pane session).
type storeSession struct {
	id        string
	directory string
	ageMs     int64
	parentID  string
	model     any
}

// liveSessionAge is a session written to just now: one a pane could still be
// running. The directory fallback ignores anything older than
// liveSessionWindowMs, so a fixture that means a live pane has to say so with a
// real timestamp rather than a counter.
const liveSessionAge int64 = 0

// historicalSessionAge is comfortably past liveSessionWindowMs: the fixture for
// a finished session that is still recorded under the pane's directory.
const historicalSessionAge = liveSessionWindowMs + 60*60*1000

// nowForTest is the wall clock a windowed read is bounded by.
func nowForTest() int64 { return time.Now().UnixMilli() }

// writeSessions builds a store holding one live top-level session per directory,
// each with a step-finish part of its own. It is the fixture for everything that
// turns on *which* session a pane may claim.
func writeSessions(t *testing.T, directories ...string) string {
	t.Helper()
	sessions := make([]storeSession, len(directories))
	for i, directory := range directories {
		sessions[i] = storeSession{
			id:        "ses_" + string(rune('a'+i)),
			directory: directory,
			ageMs:     liveSessionAge,
		}
	}
	return writeSessionRows(t, sessions...)
}

// writeSessionRows builds a store holding exactly the given sessions, each with a
// step-finish part of its own and an assistant message naming the gateway. One
// directory can therefore hold a live pane, a finished session and a subagent
// child at once, which is what the attribution rules have to tell apart.
func writeSessionRows(t *testing.T, sessions ...storeSession) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kilo.db")
	db := openStoreDB(t, dbPath)
	for _, session := range sessions {
		insertSession(t, db, session)
		insertMessage(t, db, "msg_"+session.id, session.id, 1, assistantMessage)
		insertPart(t, db, "prt_"+session.id, "msg_"+session.id, session.id, 1, stepWithModel)
	}
	closeStoreDB(t, db)
	return dbPath
}

func openStoreDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	createStoreSchema(t, db)
	return db
}

func closeStoreDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func createStoreSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE session (
		id TEXT PRIMARY KEY, directory TEXT, parent_id TEXT,
		time_updated INTEGER DEFAULT 0, time_archived INTEGER, cost REAL DEFAULT 0 NOT NULL,
		tokens_input INTEGER DEFAULT 0 NOT NULL, tokens_output INTEGER DEFAULT 0 NOT NULL,
		tokens_reasoning INTEGER DEFAULT 0 NOT NULL, tokens_cache_read INTEGER DEFAULT 0 NOT NULL,
		tokens_cache_write INTEGER DEFAULT 0 NOT NULL, model TEXT)`)
	mustExec(t, db, `CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT,
		time_created INTEGER, data TEXT)`)
	mustExec(t, db, `CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT,
		time_created INTEGER, data TEXT)`)
	mustExec(t, db, `CREATE INDEX part_session_step_finish_idx ON part (session_id)
		WHERE json_valid(part.data) AND json_extract(part.data,'$.type') = 'step-finish'`)
	mustExec(t, db, `CREATE INDEX message_session_time_created_id_idx ON message (session_id, time_created, id)`)
}

// insertSession writes one session row. A nil model writes SQL NULL, which is
// what Kilo writes for a session whose model it never recorded, and an empty
// parent id likewise writes SQL NULL for a top-level session.
func insertSession(t *testing.T, db *sql.DB, session storeSession) {
	t.Helper()
	var parentID any
	if session.parentID != "" {
		parentID = session.parentID
	}
	_, err := db.Exec(
		`INSERT INTO session (id, directory, parent_id, time_updated, model) VALUES (?, ?, ?, ?, ?)`,
		session.id, session.directory, parentID, time.Now().UnixMilli()-session.ageMs, session.model)
	if err != nil {
		t.Fatal(err)
	}
}

func insertMessage(t *testing.T, db *sql.DB, id, sessionID string, created int64, data string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?,?,?,?)`,
		id, sessionID, created, data); err != nil {
		t.Fatal(err)
	}
}

func insertPart(t *testing.T, db *sql.DB, id, messageID, sessionID string, created int64, data string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO part (id, message_id, session_id, time_created, data) VALUES (?,?,?,?,?)`,
		id, messageID, sessionID, created, data); err != nil {
		t.Fatal(err)
	}
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func useStore(t *testing.T, dbPath string) {
	t.Helper()
	t.Setenv("KILO_DB", dbPath)
	t.Setenv("KILO_DATA_DIR", "")
}

func strPtr(s string) *string { return &s }

func TestResolveUsage_NewestStepCarriesContextAndWindow(t *testing.T) {
	dir := t.TempDir()
	models := filepath.Join(dir, "models.json")
	if err := os.WriteFile(models, []byte(
		`{"kilo":{"models":{"~openai/gpt-mini-latest":{"limit":{"context":1000000}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KILO_MODELS_PATH", models)
	ClearModelsCatalogCache()

	dbPath := writeStore(t, []string{stepWithModel}, []string{assistantMessage}, "")
	useStore(t, dbPath)

	usage := ResolveUsageForKilo(strPtr("ses_test"), strPtr("/repo"))
	if usage == nil {
		t.Fatal("no usage")
	}
	// Context is prompt-cache occupancy: input + cache read + cache write. The
	// 442 output tokens are already inside the next step's input, so counting
	// them here would double-count the window.
	if usage.ContextTokens != 1219+28833 {
		t.Fatalf("context tokens = %d", usage.ContextTokens)
	}
	if usage.WindowTokens == nil || *usage.WindowTokens != 1000000 {
		t.Fatalf("window = %v", usage.WindowTokens)
	}
	if usage.Cache == nil || usage.Cache.ReadTokens != 28833 || usage.Cache.FreshInputTokens != 1219 {
		t.Fatalf("cache = %+v", usage.Cache)
	}
}

func TestResolveUsage_ModelComesFromTheMessageWhenTheStepHasNone(t *testing.T) {
	// Only 88 of Kilo's 9140 step rows carry their own model block, so the
	// message is the normal source of the backend name.
	t.Setenv("KILO_MODELS_PATH", filepath.Join(t.TempDir(), "absent.json"))
	ClearModelsCatalogCache()

	dbPath := writeStore(t, []string{stepWithoutModel}, []string{assistantMessage}, "")
	useStore(t, dbPath)

	usage := ResolveUsageForKilo(strPtr("ses_test"), strPtr("/repo"))
	if usage == nil {
		t.Fatal("no usage")
	}
	if usage.WindowTokens != nil {
		t.Fatalf("unknown model must yield no window, got %v", *usage.WindowTokens)
	}
	if backend := BackendForKilo(strPtr("ses_test"), nil); backend != "kilo" {
		t.Fatalf("backend = %q", backend)
	}
}

func TestResolveUsage_CostOnlyStepYieldsNoUsage(t *testing.T) {
	// Kilo records free-model steps with every counter at zero. Reading that as
	// a 0-token context would present as an untouched window.
	dbPath := writeStore(t, []string{costOnlyStep}, []string{assistantMessage}, "")
	useStore(t, dbPath)

	if usage := ResolveUsageForKilo(strPtr("ses_test"), strPtr("/repo")); usage != nil {
		t.Fatalf("cost-only step produced usage: %+v", usage)
	}
}

func TestResolveUsage_UnknownSessionFallsBackToCwd(t *testing.T) {
	// Herdr captures the session id at launch and never refreshes it, so a
	// cleared or resumed session reports an id that no longer exists.
	dbPath := writeStore(t, []string{stepWithModel}, []string{assistantMessage}, "")
	useStore(t, dbPath)

	if usage := ResolveUsageForKilo(strPtr("ses_stale"), strPtr("/repo")); usage == nil {
		t.Fatal("cwd fallback did not recover the session")
	}
	if usage := ResolveUsageForKilo(strPtr("ses_stale"), strPtr("/elsewhere")); usage != nil {
		t.Fatalf("wrong cwd resolved usage: %+v", usage)
	}
}

func TestResolveUsage_NoIdentifiersYieldsNothing(t *testing.T) {
	dbPath := writeStore(t, []string{stepWithModel}, []string{assistantMessage}, "")
	useStore(t, dbPath)

	if usage := ResolveUsageForKilo(nil, nil); usage != nil {
		t.Fatalf("no identifiers produced usage: %+v", usage)
	}
	if usage := ResolveUsageForKilo(strPtr(""), strPtr("")); usage != nil {
		t.Fatalf("blank identifiers produced usage: %+v", usage)
	}
}

func TestResolveUsage_TwoPanesInOneRepoDoNotCrossAttribute(t *testing.T) {
	// Two live Kilo panes in one repository share a cwd, so a pane whose
	// reported id is gone must not borrow the other pane's session by directory.
	// Once the id is stale the newest row in that directory is the other pane's,
	// so recovering from it would report a confident, wrong reading — for
	// context and for billing alike.
	dbPath := writeSessions(t, "/repo", "/repo")
	useStore(t, dbPath)

	// A pinned id is still its own session.
	if usage := ResolveUsageForKilo(strPtr("ses_a"), nil); usage == nil {
		t.Fatal("id-only resolution failed")
	}
	// A directory holding two live sessions attributes nothing.
	if usage := ResolveUsageForKilo(strPtr("ses_gone"), strPtr("/repo")); usage != nil {
		t.Fatalf("an ambiguous directory attributed another pane's session: %+v", usage)
	}
	if usage := ResolveUsageForKilo(nil, strPtr("/repo")); usage != nil {
		t.Fatalf("an ambiguous directory attributed a session: %+v", usage)
	}
	if backend := BackendForKilo(strPtr("ses_gone"), strPtr("/repo")); backend != "" {
		t.Fatalf("billing mode named a backend for an unattributable pane: %q", backend)
	}
}

func TestResolveUsage_CwdFallbackStaysInsideTheDirectoryTree(t *testing.T) {
	// The fallback reaches the directory and its descendants, which is how a
	// worktree checked out under the repo is covered. A bare prefix would also
	// reach siblings, so /repo must never answer for /repo-other; and without an
	// ESCAPE clause SQLite reads "_" and "%" in a path as wildcards.
	t.Setenv("KILO_MODELS_PATH", filepath.Join(t.TempDir(), "absent.json"))
	ClearModelsCatalogCache()

	t.Run("a descendant resolves", func(t *testing.T) {
		useStore(t, writeSessions(t, "/repo/worktrees/wt"))
		if usage := ResolveUsageForKilo(strPtr("ses_gone"), strPtr("/repo")); usage == nil {
			t.Fatal("a worktree under the directory did not resolve")
		}
	})
	t.Run("a sibling does not", func(t *testing.T) {
		// Two stores, because the point is that /repo-other is out of scope for
		// a pane in /repo: an in-scope /repo session must win, and with none,
		// nothing may be invented from the sibling.
		useStore(t, writeSessions(t, "/repo", "/repo-other"))
		if usage := ResolveUsageForKilo(strPtr("ses_gone"), strPtr("/repo")); usage == nil {
			t.Fatal("the session in the pane's own directory did not resolve")
		}
		useStore(t, writeSessions(t, "/repo-other"))
		if usage := ResolveUsageForKilo(strPtr("ses_gone"), strPtr("/repo")); usage != nil {
			t.Fatalf("a sibling directory resolved: %+v", usage)
		}
	})
	t.Run("a wildcard in the path is a literal", func(t *testing.T) {
		for _, directory := range []string{"/my_repo", "/100%done"} {
			useStore(t, writeSessions(t, filepath.Join(directory, "nested")))
			if usage := ResolveUsageForKilo(strPtr("ses_gone"), strPtr(directory)); usage == nil {
				t.Fatalf("%s: a descendant of a wildcarded path did not resolve", directory)
			}
		}
	})
	t.Run("a wildcard in the path cannot invent a match", func(t *testing.T) {
		// "/my_repo" must not stand in for "/myXrepo": the underscore is a path
		// character here, not a single-character wildcard.
		useStore(t, writeSessions(t, "/myXrepo"))
		if usage := ResolveUsageForKilo(strPtr("ses_gone"), strPtr("/my_repo")); usage != nil {
			t.Fatalf("an underscore was read as a wildcard: %+v", usage)
		}
	})
}

func TestBackendForKilo_RecoversTheSessionByDirectory(t *testing.T) {
	// Billing mode and context must name the same session. When herdr's id has
	// gone stale, a pane that shows a Kilo context but an empty backend would be
	// classified as Unknown and could appear next to the Kilo allowance instead of
	// as the pay-as-you-go session it is.
	dbPath := writeSessions(t, "/repo")
	useStore(t, dbPath)

	if backend := BackendForKilo(strPtr("ses_gone"), strPtr("/repo")); backend != "kilo" {
		t.Fatalf("backend = %q, want the recovered session's", backend)
	}
	if backend := BackendForKilo(strPtr("ses_gone"), nil); backend != "" {
		t.Fatalf("a session that cannot be resolved produced a backend: %q", backend)
	}
}

func TestResolveUsage_AbsentStoreYieldsNothing(t *testing.T) {
	t.Setenv("KILO_DB", filepath.Join(t.TempDir(), "absent.db"))
	if usage := ResolveUsageForKilo(strPtr("ses_test"), strPtr("/repo")); usage != nil {
		t.Fatalf("absent store produced usage: %+v", usage)
	}
	t.Setenv("KILO_DB", "")
	t.Setenv("KILO_DATA_DIR", t.TempDir())
	if usage := ResolveUsageForKilo(strPtr("ses_test"), strPtr("/repo")); usage != nil {
		t.Fatalf("empty data dir produced usage: %+v", usage)
	}
}

func TestResolveUsage_ModelComesFromTheMessageThatOwnsTheStep(t *testing.T) {
	// After a model switch the newest assistant message can belong to a call
	// that has not completed a step yet. Reading the identity from the session's
	// newest message would pair the previous model's tokens with the new model's
	// window — a false low percentage, or an overflow.
	models := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(models, []byte(
		`{"kilo":{"models":{"~openai/gpt-mini-latest":{"limit":{"context":1000000}}}},
		   "anthropic":{"models":{"claude-sonnet-5":{"limit":{"context":200000}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KILO_MODELS_PATH", models)
	ClearModelsCatalogCache()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kilo.db")
	db := openStoreDB(t, dbPath)
	insertSession(t, db, storeSession{id: "ses_test", directory: "/repo", ageMs: liveSessionAge})
	// The step belongs to the earlier message and was served by the earlier model.
	insertMessage(t, db, "msg_a", "ses_test", 1, assistantMessage)
	insertPart(t, db, "prt_a", "msg_a", "ses_test", 1, stepWithoutModel)
	// A newer turn switched model and has not completed a step.
	insertMessage(t, db, "msg_b", "ses_test", 2,
		`{"role":"assistant","providerID":"anthropic","modelID":"claude-sonnet-5",
		  "tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}}}`)
	closeStoreDB(t, db)
	useStore(t, dbPath)

	usage := ResolveUsageForKilo(strPtr("ses_test"), strPtr("/repo"))
	if usage == nil {
		t.Fatal("no usage")
	}
	// The step's own model, not the pane's newest one.
	if usage.WindowTokens == nil || *usage.WindowTokens != 1000000 {
		t.Fatalf("window = %v, want the step's own model", usage.WindowTokens)
	}
	// Billing mode still follows the live session, which is the newer message.
	if backend := BackendForKilo(strPtr("ses_test"), strPtr("/repo")); backend != "anthropic" {
		t.Fatalf("live backend = %q, want the newest assistant message's", backend)
	}
}

func TestResolveUsage_CwdFallbackIgnoresSessionsNoPaneCanBeUsing(t *testing.T) {
	// "Recorded in this directory" is not the question the fallback is asking.
	// A subagent child runs inside its parent's process and no pane ever
	// launches one, and a session nobody has written to for a working day is
	// history. Counting either makes a directory with exactly one pane in it look
	// shared, which costs that pane its context and its billing the moment its
	// captured id goes stale — which is the only case the fallback exists for.
	live := storeSession{id: "ses_live", directory: "/repo", ageMs: liveSessionAge}

	t.Run("a subagent child is not a pane", func(t *testing.T) {
		useStore(t, writeSessionRows(t,
			live,
			storeSession{id: "ses_child", directory: "/repo", ageMs: liveSessionAge, parentID: "ses_live"},
		))
		if usage := ResolveUsageForKilo(strPtr("ses_gone"), strPtr("/repo")); usage == nil {
			t.Fatal("the directory's one pane was hidden by its own subagent child")
		}
		if backend := BackendForKilo(strPtr("ses_gone"), strPtr("/repo")); backend != "kilo" {
			t.Fatalf("billing named %q behind a subagent child", backend)
		}
		if rows := PaneSpend(strPtr("ses_gone"), strPtr("/repo"), "kilo", 0, nowForTest()); len(rows) == 0 {
			t.Fatal("pane spend was hidden by its own subagent child")
		}
	})

	t.Run("a finished session is not a pane", func(t *testing.T) {
		useStore(t, writeSessionRows(t,
			live,
			storeSession{id: "ses_old", directory: "/repo", ageMs: historicalSessionAge},
		))
		if usage := ResolveUsageForKilo(strPtr("ses_gone"), strPtr("/repo")); usage == nil {
			t.Fatal("the directory's one pane was hidden by a session from yesterday")
		}
	})

	t.Run("two real panes are still ambiguous", func(t *testing.T) {
		// The narrowing is only allowed to drop rows that cannot be the answer.
		// Two live top-level sessions are two panes, and neither may claim the
		// other's context, backend or spend.
		useStore(t, writeSessionRows(t,
			live,
			storeSession{id: "ses_second", directory: "/repo", ageMs: liveSessionAge},
			storeSession{id: "ses_child", directory: "/repo", ageMs: liveSessionAge, parentID: "ses_live"},
			storeSession{id: "ses_old", directory: "/repo", ageMs: historicalSessionAge},
		))
		if usage := ResolveUsageForKilo(strPtr("ses_gone"), strPtr("/repo")); usage != nil {
			t.Fatalf("two live panes were narrowed down to one: %+v", usage)
		}
		if backend := BackendForKilo(strPtr("ses_gone"), strPtr("/repo")); backend != "" {
			t.Fatalf("billing named %q for two unattributable panes", backend)
		}
		if rows := PaneSpend(strPtr("ses_gone"), strPtr("/repo"), "kilo", 0, nowForTest()); rows != nil {
			t.Fatalf("two unattributable panes reported spend: %d rows", len(rows))
		}
	})
}

func TestQueriesAreBoundedAndReadOnly(t *testing.T) {
	// A whole-table scan would be a bug: the store is a live WAL database with
	// hundreds of thousands of rows.
	for _, query := range []string{stepQuery, identityQuery, paneSpendQuery} {
		if !contains(query, "session_id = ?") && !contains(query, "WHERE id = ?") {
			t.Fatalf("query is not scoped to one session: %s", query)
		}
	}
	if !contains(stepQuery, "LIMIT ?") || !contains(identityQuery, "LIMIT 1") {
		t.Fatalf("queries are unbounded: %s / %s", stepQuery, identityQuery)
	}
	if contains(stepQuery, "SUM(") {
		t.Fatalf("step query aggregates instead of reading the newest row")
	}
	// The step's identity is read from one named message, so it is keyed on the
	// primary key rather than on a session-wide "newest" scan.
	if !contains(partIdentityQuery, "WHERE m.id = ?") || !contains(partIdentityQuery, "LIMIT 1") {
		t.Fatalf("step identity is not keyed on one message: %s", partIdentityQuery)
	}
	// The directory fallback must stay bounded and must keep its two-row read:
	// deciding whether a directory is shared needs to see that it is.
	if !contains(directoryScopeQuery, "LIMIT 2") {
		t.Fatalf("directory scope cannot detect ambiguity: %s", directoryScopeQuery)
	}
	if !contains(directoryScopeQuery, "ESCAPE") {
		t.Fatalf("directory scope reads wildcards as path characters: %s", directoryScopeQuery)
	}
	// The scope is the directory plus a separator-bounded prefix, never a bare
	// prefix, which would also match sibling directories.
	if !contains(directoryScopeQuery, "directory = ?") || !contains(directoryScopeQuery, "directory LIKE ?") {
		t.Fatalf("directory scope lost the exact match: %s", directoryScopeQuery)
	}
	// And it must keep excluding the two kinds of row that cannot be a pane.
	if !contains(directoryScopeQuery, "parent_id IS NULL") {
		t.Fatalf("directory scope counts subagent children as panes: %s", directoryScopeQuery)
	}
	if !contains(directoryScopeQuery, "time_updated >= ?") {
		t.Fatalf("directory scope counts finished sessions as panes: %s", directoryScopeQuery)
	}
	// The account-wide spend scan has to be driven from the session table. A
	// plain JOIN lets SQLite pick the multi-million-row message table as the
	// outer loop, which is a whole-store scan on every panel refresh.
	if !contains(accountSpendQuery, "CROSS JOIN") {
		t.Fatalf("account spend does not pin the driving table: %s", accountSpendQuery)
	}
	for _, query := range []string{accountSpendQuery, paneSpendQuery} {
		if !contains(query, "m.time_created >= ?") {
			t.Fatalf("spend query is not bounded by the window: %s", query)
		}
		if !contains(query, "$.role') = 'assistant") {
			t.Fatalf("spend query counts a non-assistant message: %s", query)
		}
		// The session row's own cost and token columns are lifetime totals for
		// every backend the session used, so they must never answer what one
		// backend spent.
		if contains(query, "tokens_") || contains(query, "s.cost") {
			t.Fatalf("spend query reads the session totals instead of per-backend messages: %s", query)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
