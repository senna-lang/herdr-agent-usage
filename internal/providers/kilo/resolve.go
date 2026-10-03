/**
 * Resolves context usage for one Kilo pane from Kilo's session store.
 *
 * Context comes from the newest completed "step-finish" part of that session,
 * which is the row Kilo itself uses for its "Token Usage" panel. Reading the
 * step rather than summing a message's counters matters: a step carries the
 * exact prompt-cache occupancy for that request, while a message's counters
 * mix in output and reasoning tokens that the next step has already absorbed.
 *
 * Every read is bounded and read-only. Kilo's database is a live WAL database
 * with a partial index over exactly this query, so it is opened with mode=ro
 * and never scanned whole.
 */
package kilo

import (
	"database/sql"
	"path/filepath"
	"strings"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	_ "modernc.org/sqlite"
)

// stepScanLimit bounds the step-finish tail read for one session. The newest
// row is all that is needed; the rest exist so that a session whose newest step
// was compacted or truncated still resolves from an earlier one.
const stepScanLimit = 24

// liveSessionWindowMs is how recently a session must have been written to for a
// directory to still consider it a live pane's.
//
// It only ever filters the cwd fallback, never a pane's own reported id: a pane
// whose id still resolves is attributed however old its session is. The value is
// a compromise between the two ways being wrong — too long and yesterday's
// finished session makes today's single pane look shared, too short and a pane
// left open overnight stops reporting — so it spans a working day rather than a
// working shift.
const liveSessionWindowMs = 12 * 60 * 60 * 1000

// stepQuery reads the tail of a session's step-finish rows. The owning message
// id is selected alongside the payload because that message — not the session's
// newest assistant message — is what names the model this step was served by.
const stepQuery = `
	SELECT p.data, COALESCE(p.message_id, '')
	FROM part p
	WHERE p.session_id = ?
	  AND json_extract(p.data, '$.type') = 'step-finish'
	ORDER BY p.time_created DESC
	LIMIT ?`

// identityQuery names the backend a session currently runs on. This is the
// pane's *live* backend, so "newest assistant message in the session" is the
// right question here.
const identityQuery = `
	SELECT m.data
	FROM message m
	WHERE m.session_id = ?
	  AND json_extract(m.data, '$.role') = 'assistant'
	  AND json_extract(m.data, '$.providerID') IS NOT NULL
	ORDER BY m.time_created DESC
	LIMIT 1`

// partIdentityQuery names the backend of one specific step, by the message that
// owns it. Keying on the message id rather than the session is what keeps a
// completed step paired with the model that actually produced it.
const partIdentityQuery = `
	SELECT m.data
	FROM message m
	WHERE m.id = ?
	  AND json_valid(m.data)
	LIMIT 1`

// directoryScopeQuery lists the sessions a pane working in one directory could
// be using: that directory itself, or anything beneath it, which is how a
// worktree checked out under the repository is covered.
//
// "Could be using" is narrower than "recorded in that directory", and each
// predicate narrows it deliberately:
//
//   - parent_id IS NULL keeps subagent child sessions out. A child runs inside
//     its parent's process and no pane ever launches one, so counting it makes a
//     directory look shared when exactly one pane is in it.
//   - time_updated is the liveness evidence. A session nobody has written to for
//     longer than liveSessionWindowMs is history, not a pane: leaving it in
//     scope is what makes a directory ambiguous over a session that cannot be
//     running, which costs the pane its own context and billing the moment its
//     captured id goes stale.
//
// It deliberately reads two rows. The caller has to be able to tell an
// unambiguous match from a shared one, which a LIMIT 1 newest-first query
// cannot: two panes in one repository share a cwd, and after one of them resets
// its session the newest row belongs to the other pane.
//
// The child arm is separator-bounded and carries an ESCAPE clause, so /repo
// never reaches /repo-other and a directory containing "_" or "%" is a literal
// rather than a wildcard.
const directoryScopeQuery = `
	SELECT id FROM session
	WHERE time_archived IS NULL
	  AND parent_id IS NULL
	  AND time_updated >= ?
	  AND (directory = ? OR directory LIKE ? ESCAPE '\')
	ORDER BY time_updated DESC
	LIMIT 2`

func openReadonlyDB(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+path+"?mode=ro")
}

// ResolveUsageForKilo resolves context usage from a pane's session.
func ResolveUsageForKilo(sessionID, cwd *string) *core.ContextUsage {
	dbPath := ResolveKiloDBPath()
	if dbPath == "" {
		return nil
	}
	return resolveUsageIn(dbPath, sessionID, cwd)
}

// ResolveUsageForKiloIn resolves usage from one configured data directory and
// never consults KILO_DB or KILO_DATA_DIR.
func ResolveUsageForKiloIn(dataDir string, sessionID, cwd *string) *core.ContextUsage {
	dbPath := ResolveKiloDBPathIn(dataDir)
	if dbPath == "" {
		return nil
	}
	return resolveUsageIn(dbPath, sessionID, cwd)
}

func resolveUsageIn(dbPath string, sessionID, cwd *string) *core.ContextUsage {
	db, err := openReadonlyDB(dbPath)
	if err != nil {
		return nil
	}
	defer db.Close()

	id := resolveSessionIDIn(db, sessionID, cwd)
	if id == "" {
		return nil
	}

	row := latestStepRow(db, id)
	if row == nil {
		return nil
	}
	step := row.Usage
	if step.ProviderID == "" && row.MessageID != "" {
		// Only 88 of Kilo's 9140 step rows carry their own model block, so the
		// identity almost always comes from a message. It must be the message
		// that owns *this* step: after a model switch the newest assistant
		// message can belong to a call that has not completed yet, and pairing
		// this step's tokens with that message's model would divide one model's
		// context by another model's window.
		if identity := messageIdentityForPart(db, row.MessageID); identity.ProviderID != "" {
			step.ProviderID = identity.ProviderID
			if step.ModelID == "" {
				step.ModelID = identity.ModelID
			}
		}
	}

	usage := core.ContextUsage{
		ContextTokens: step.ContextTokens,
		Cache:         core.CacheFromTokenCounts(step.CacheFresh, step.CacheRead, step.CacheWrite),
		SessionCache:  sessionCache(db, id, step),
	}
	if window := ContextWindowFor(step.ProviderID, step.ModelID); window != nil {
		usage.WindowTokens = window
	}
	return &usage
}

// resolveSessionIDIn takes the reported session id when it still resolves, and
// otherwise falls back to the pane's directory.
//
// It is the single place a pane's session is chosen, and context, billing mode
// and pane activity all go through it, so a pane's context, backend and spend
// can never come from three different sessions.
//
// Herdr captures the session id at launch and never refreshes it, so a cleared
// or resumed session reports an id that no longer exists. The directory is a
// fallback, never a first choice: two Kilo panes in one repository share a cwd,
// so the directory only names a session when it leaves no room for doubt.
func resolveSessionIDIn(db *sql.DB, sessionID, cwd *string) string {
	id := ""
	if sessionID != nil {
		id = strings.TrimSpace(*sessionID)
	}
	if id != "" {
		var found int
		if err := db.QueryRow(`SELECT 1 AS ok FROM session WHERE id = ? LIMIT 1`, id).Scan(&found); err == nil {
			return id
		}
	}
	// An empty cwd is no identifier at all, and must not reach the fallback.
	if cwd == nil {
		return ""
	}
	return resolveSessionIDByCwd(db, *cwd)
}

// resolveSessionIDByCwd returns the one live session a pane working in cwd can
// be attributed to, and "" whenever attribution cannot be established.
//
// Two live Kilo panes in one repository share a cwd, so "the newest session in
// this directory" is not evidence of which pane asked: once one pane resets its
// session, the newest row is the other pane's, and the recovered pane would
// report another pane's context and backend. A directory therefore attributes a
// session only when exactly one live session is in scope. Ambiguity yields no
// reading rather than a confidently wrong one.
//
// "Live" is the directoryScopeQuery scope rather than "not archived": a
// subagent child and a session last touched days ago are both recorded under
// the same directory and neither is a pane, so counting either would make a
// directory ambiguous over rows that cannot be the answer.
func resolveSessionIDByCwd(db *sql.DB, cwd string) string {
	directory := normalizeDirectory(cwd)
	if directory == "" {
		return ""
	}
	floor := time.Now().UnixMilli() - liveSessionWindowMs
	separator := string(filepath.Separator)
	rows, err := db.Query(directoryScopeQuery, floor, directory, escapeLike(directory+separator)+"%")
	if err != nil {
		return ""
	}
	defer rows.Close()
	found := ""
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if found != "" {
			// Two panes could own these sessions; neither may claim the
			// other's, so neither gets a reading.
			return ""
		}
		found = id
	}
	return found
}

// normalizeDirectory reduces a pane cwd to the absolute directory form Kilo
// records, so a trailing separator or a "." segment cannot turn one directory
// into two spellings that each match differently.
func normalizeDirectory(cwd string) string {
	trimmed := strings.TrimSpace(cwd)
	if trimmed == "" {
		return ""
	}
	// A relative "." names no directory Kilo could have recorded.
	if cleaned := filepath.Clean(trimmed); cleaned != "." {
		return cleaned
	}
	return ""
}

// escapeLike neutralises the wildcards in a directory path before it is used in
// a LIKE comparison, so a repo checked out under a directory containing "_" or
// "%" cannot match an unrelated session. It requires the ESCAPE clause that
// directoryScopeQuery carries: SQLite's LIKE has no implicit backslash escape.
func escapeLike(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(value)
}

// stepRow is one scanned part row: the decoded usage, plus the id of the
// message that owns the step.
type stepRow struct {
	Usage     *StepUsage
	MessageID string
}

func latestStepRow(db *sql.DB, sessionID string) *stepRow {
	rows, err := db.Query(stepQuery, sessionID, stepScanLimit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var raw, messageID string
		if err := rows.Scan(&raw, &messageID); err != nil {
			continue
		}
		if usage := ParseStepUsage(raw); usage != nil {
			return &stepRow{Usage: usage, MessageID: strings.TrimSpace(messageID)}
		}
	}
	return nil
}

func latestMessageIdentity(db *sql.DB, sessionID string) MessageIdentity {
	rows, err := db.Query(identityQuery, sessionID)
	if err != nil {
		return MessageIdentity{}
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		return ParseMessageIdentity(raw)
	}
	return MessageIdentity{}
}

// messageIdentityForPart reads the identity of the message that owns one step.
func messageIdentityForPart(db *sql.DB, messageID string) MessageIdentity {
	var raw string
	if err := db.QueryRow(partIdentityQuery, messageID).Scan(&raw); err != nil {
		return MessageIdentity{}
	}
	return ParseMessageIdentity(raw)
}

// sessionCache sums the prompt-cache counters across the scanned step tail.
// Only the steps actually read are counted, so the figure is a bounded sample
// of the transcript rather than a claim about the whole session; the latest
// turn's own figures stay in Cache.
func sessionCache(db *sql.DB, sessionID string, newest *StepUsage) *core.CacheUsage {
	rows, err := db.Query(stepQuery, sessionID, stepScanLimit)
	if err != nil {
		return core.CacheFromTokenCounts(newest.CacheFresh, newest.CacheRead, newest.CacheWrite)
	}
	defer rows.Close()
	fresh, read, write := 0, 0, 0
	for rows.Next() {
		var raw, messageID string
		if err := rows.Scan(&raw, &messageID); err != nil {
			continue
		}
		if usage := ParseStepUsage(raw); usage != nil {
			fresh += usage.CacheFresh
			read += usage.CacheRead
			write += usage.CacheWrite
		}
	}
	return core.CacheFromTokenCounts(fresh, read, write)
}

// BackendForKilo reports which backend served a pane's session.
//
// The pane's session is resolved exactly as context resolution resolves it, so
// a pane whose reported id is gone is still classified by the session its
// context comes from. Resolving the id separately here would leave that pane
// displaying a Kilo context while its billing mode saw no backend at all, which
// is how a foreign API-key session ends up showing alongside a Kilo allowance
// instead of as pay-as-you-go.
func BackendForKilo(sessionID, cwd *string) string {
	dbPath := ResolveKiloDBPath()
	if dbPath == "" {
		return ""
	}
	db, err := openReadonlyDB(dbPath)
	if err != nil {
		return ""
	}
	defer db.Close()
	id := resolveSessionIDIn(db, sessionID, cwd)
	if id == "" {
		return ""
	}
	return latestMessageIdentity(db, id).ProviderID
}
