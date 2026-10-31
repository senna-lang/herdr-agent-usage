/**
 * Reads one Devin CLI session from its local store.
 *
 * Devin CLI keeps every session in a single SQLite database
 * (cli/sessions.db) that it owns and can grow to tens of gigabytes, so the
 * adapter opens it read-only and only ever resolves through indexed single
 * rows: it must not scan a session's node forest on the refresh path,
 * because chat_message carries full message payloads (~35KB per node) and a
 * long session holds hundreds of thousands of nodes.
 *
 * metrics.context_input_tokens is the context occupying the window at that
 * request (fresh input plus cached input), which is exactly what the
 * sidebar's context meter needs; no replay of the transcript is required.
 *
 * What this file deliberately does not read: session-wide aggregates
 * (lifetime tokens, windowed activity). Devin writes one node per streamed
 * update of a request, so any aggregate must dedupe by
 * metadata.request_id — a GROUP BY that parses every node's chat_message.
 * On a multi-GB store that is seconds of CPU and gigabytes of page traffic
 * per invocation, and it stacks up under pane events. Those numbers need an
 * incrementally maintained index before they can be served cheaply; see
 * docs/devin-contract.md.
 */
package devin

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// latestMetrics is the most recent request's recorded usage for a session.
type latestMetrics struct {
	ContextInput int
	Input        int
	Output       int
	CacheRead    int
	CacheCreate  int
	Model        string
	CreatedAtSec int64
}

func openSessionsDB(path string) *sql.DB {
	if path == "" {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return nil
	}
	return db
}

func sessionExists(db *sql.DB, sessionID string) bool {
	var found string
	err := db.QueryRow(`SELECT id FROM sessions WHERE id = ? LIMIT 1`, sessionID).Scan(&found)
	return err == nil
}

// readLatestMetrics returns the newest request's metrics. row_id is the
// table's INTEGER PRIMARY KEY, so descending it is a btree descent, not a
// sort: Devin appends one node per streamed update and the newest
// metrics-bearing row is the answer the sidebar wants.
func readLatestMetrics(db *sql.DB, sessionID string) (*latestMetrics, bool) {
	var m latestMetrics
	var cacheCreate sql.NullInt64
	err := db.QueryRow(`SELECT
		COALESCE(json_extract(chat_message,'$.metadata.metrics.context_input_tokens'),0),
		COALESCE(json_extract(chat_message,'$.metadata.metrics.input_tokens'),0),
		COALESCE(json_extract(chat_message,'$.metadata.metrics.output_tokens'),0),
		COALESCE(json_extract(chat_message,'$.metadata.metrics.cache_read_tokens'),0),
		json_extract(chat_message,'$.metadata.metrics.cache_creation_tokens'),
		COALESCE(json_extract(chat_message,'$.metadata.generation_model'),''),
		created_at
	FROM message_nodes
	WHERE session_id = ?
	  AND json_valid(chat_message)
	  AND json_extract(chat_message,'$.metadata.metrics.context_input_tokens') IS NOT NULL
	ORDER BY row_id DESC
	LIMIT 1`, sessionID).Scan(&m.ContextInput, &m.Input, &m.Output, &m.CacheRead, &cacheCreate, &m.Model, &m.CreatedAtSec)
	if err != nil {
		return nil, false
	}
	if cacheCreate.Valid {
		m.CacheCreate = int(cacheCreate.Int64)
	}
	return &m, true
}
