/**
 * Reads one Devin CLI session from its local store.
 *
 * Devin CLI keeps every session in a single SQLite database
 * (cli/sessions.db) that it owns and can grow to tens of gigabytes, so the
 * adapter opens it read-only, never scans it whole, and only ever filters
 * through the per-session index or the sessions table itself.
 *
 * Sessions are node forests: message_nodes.chat_message holds one chat
 * message per node, and assistant nodes record their request under
 * $.metadata.request_id with usage under $.metadata.metrics. The same
 * request_id appears on several sibling nodes carrying the same metrics
 * (Devin writes one node per streamed update), so every aggregate dedupes
 * by request_id — summing raw rows would multiply-count a request.
 *
 * metrics.context_input_tokens is the context occupying the window at that
 * request (fresh input plus cached input), which is exactly what the
 * sidebar's context meter needs; no replay of the transcript is required.
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

// sessionTotals is a session's deduplicated lifetime usage.
type sessionTotals struct {
	Input       int
	Output      int
	CacheRead   int
	CacheCreate int
	Requests    int
}

// Sum returns every context-touching token the session billed.
func (t sessionTotals) Sum() int {
	return t.Input + t.Output + t.CacheRead + t.CacheCreate
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

// readLatestMetrics returns the newest request's metrics. node_id and
// created_at can both repeat across a tree, so recency follows row_id, the
// database's own insertion order.
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

// metricColumns is the SELECT list shared by every request-deduping
// aggregate below. MAX per request_id keeps one copy of the metrics Devin
// stamps on each sibling node of the same request.
const metricColumns = `
	MAX(COALESCE(json_extract(m.chat_message,'$.metadata.metrics.input_tokens'),0)) AS in_tok,
	MAX(COALESCE(json_extract(m.chat_message,'$.metadata.metrics.output_tokens'),0)) AS out_tok,
	MAX(COALESCE(json_extract(m.chat_message,'$.metadata.metrics.cache_read_tokens'),0)) AS cache_read,
	MAX(COALESCE(json_extract(m.chat_message,'$.metadata.metrics.cache_creation_tokens'),0)) AS cache_create`

// readSessionTotals aggregates the session's lifetime usage, deduplicated
// by request_id (see the file header for why raw sums are wrong).
func readSessionTotals(db *sql.DB, sessionID string) sessionTotals {
	var t sessionTotals
	err := db.QueryRow(`WITH reqs AS (
		SELECT `+metricColumns+`
		FROM message_nodes m
		WHERE m.session_id = ?
		  AND json_valid(m.chat_message)
		  AND json_extract(m.chat_message,'$.metadata.metrics.input_tokens') IS NOT NULL
		GROUP BY json_extract(m.chat_message,'$.metadata.request_id')
	)
	SELECT COALESCE(SUM(in_tok),0), COALESCE(SUM(out_tok),0),
	       COALESCE(SUM(cache_read),0), COALESCE(SUM(cache_create),0), COUNT(*)
	FROM reqs`, sessionID).Scan(&t.Input, &t.Output, &t.CacheRead, &t.CacheCreate, &t.Requests)
	if err != nil {
		return sessionTotals{}
	}
	return t
}

// windowedTokens sums input+output tokens for requests created inside the
// window (epoch seconds, inclusive), deduplicated by request_id. Cache
// reads are excluded: activity share tracks new generation, matching the
// input+output rows the other providers count.
func windowedTokens(db *sql.DB, sessionID string, startSec, endSec int64) float64 {
	var total sql.NullFloat64
	err := db.QueryRow(`WITH reqs AS (
		SELECT `+metricColumns+`
		FROM message_nodes m
		WHERE m.session_id = ?
		  AND m.created_at >= ? AND m.created_at <= ?
		  AND json_valid(m.chat_message)
		  AND json_extract(m.chat_message,'$.metadata.metrics.input_tokens') IS NOT NULL
		GROUP BY json_extract(m.chat_message,'$.metadata.request_id')
	)
	SELECT SUM(in_tok + out_tok) FROM reqs`, sessionID, startSec, endSec).Scan(&total)
	if err != nil || !total.Valid {
		return 0
	}
	return total.Float64
}

// SessionTokensInWindow sums one session's fresh tokens in the window.
// Timestamps arrive in milliseconds and convert to the store's seconds.
// An empty dataDir resolves the default location.
func SessionTokensInWindow(dataDir, sessionID string, startMs, endMs int64) float64 {
	if sessionID == "" {
		return 0
	}
	if dataDir == "" {
		dataDir = ResolveDevinDataDir()
	}
	db := openSessionsDB(ResolveDevinSessionsDBPathIn(dataDir))
	if db == nil {
		return 0
	}
	defer db.Close()
	return windowedTokens(db, sessionID, startMs/1000, endMs/1000)
}

// TotalTokensInWindow sums fresh tokens across every session on disk in the
// window. A session cannot have requests newer than its own
// last_activity_at, so sessions idle since before the window are skipped
// before their message trees are opened.
func TotalTokensInWindow(dataDir string, startMs, endMs int64) float64 {
	if dataDir == "" {
		dataDir = ResolveDevinDataDir()
	}
	db := openSessionsDB(ResolveDevinSessionsDBPathIn(dataDir))
	if db == nil {
		return 0
	}
	defer db.Close()
	startSec, endSec := startMs/1000, endMs/1000
	rows, err := db.Query(`SELECT id FROM sessions WHERE last_activity_at >= ?`, startSec)
	if err != nil {
		return 0
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	var total float64
	for _, id := range ids {
		total += windowedTokens(db, id, startSec, endSec)
	}
	return total
}
