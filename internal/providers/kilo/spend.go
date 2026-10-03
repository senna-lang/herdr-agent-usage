/**
 * Backend-scoped spend reads from Kilo's session store.
 *
 * Kilo can switch a session's backend mid-way — the Kilo Gateway for a while,
 * then a vendor Kilo holds a key for — and the sidebar labels a pane by the
 * backend it is on now. The session row's own totals cannot answer that: they
 * are lifetime figures that mix every backend the session ever used, so reading
 * them charges the earlier backend's spend to the later one's label. The
 * assistant messages carry the backend that served each turn, so the spend is
 * summed per backend instead.
 *
 * Only the message envelopes are returned. Decoding tokens and cost is the
 * shared pay-as-you-go decoder's job, and Kilo writes the same envelope OpenCode
 * does, so neither harness needs a decoder of its own.
 */
package kilo

import "strings"

// AssistantMessage is one assistant message row: the raw JSON body plus the
// row's own creation time, which the shared decoder prefers over any timestamp
// inside the body.
type AssistantMessage struct {
	Data        string
	TimeCreated int64
}

// accountSpendQuery reads every assistant message any Kilo session recorded in
// the window, grouped by the backend that served it.
//
// It is driven from the session table, not the message table: the store is a
// live multi-gigabyte WAL database, while session is a few thousand rows and
// names every session that can hold a message in the window. CROSS JOIN pins
// that order — a plain JOIN lets SQLite pick the message table as the outer
// loop, which is a whole-table scan — and each session is then read through
// message_session_time_created_id_idx, so no part of this is unbounded.
//
// Subagent child sessions are included on purpose. Their spend is drawn on the
// same credential as the pane that spawned them, and this block is
// account-wide, so omitting it would understate the backend.
const accountSpendQuery = `
	SELECT json_extract(m.data, '$.providerID'), m.data, m.time_created
	FROM session s
	CROSS JOIN message m ON m.session_id = s.id
	WHERE s.time_updated >= ? AND s.time_updated <= ?
	  AND m.time_created >= ? AND m.time_created <= ?
	  AND json_valid(m.data)
	  AND json_extract(m.data, '$.role') = 'assistant'
	  AND json_extract(m.data, '$.providerID') IS NOT NULL`

// paneSpendQuery is accountSpendQuery narrowed to one pane's own session and
// one backend, which is the read behind a pane's own spend line.
const paneSpendQuery = `
	SELECT m.data, m.time_created
	FROM message m
	WHERE m.session_id = ?
	  AND m.time_created >= ? AND m.time_created <= ?
	  AND json_valid(m.data)
	  AND json_extract(m.data, '$.role') = 'assistant'
	  AND json_extract(m.data, '$.providerID') = ?`

// SpendByBackend returns the assistant messages each backend served across every
// session touched inside the window, keyed by backend id.
//
// A store that cannot be opened or read yields nothing rather than a partial
// figure: the caller draws a spend total from this, and a partial total reads as
// a real one.
func SpendByBackend(startMs, endMs int64) map[string][]AssistantMessage {
	dbPath := ResolveKiloDBPath()
	if dbPath == "" {
		return nil
	}
	db, err := openReadonlyDB(dbPath)
	if err != nil {
		return nil
	}
	defer db.Close()

	rows, err := db.Query(accountSpendQuery, startMs, endMs, startMs, endMs)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := map[string][]AssistantMessage{}
	for rows.Next() {
		var backendID string
		var message AssistantMessage
		if err := rows.Scan(&backendID, &message.Data, &message.TimeCreated); err != nil {
			continue
		}
		backendID = strings.TrimSpace(backendID)
		if backendID == "" {
			continue
		}
		out[backendID] = append(out[backendID], message)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// PaneSpend returns the messages one backend served in the pane's own session
// inside the window.
//
// The pane's session is resolved exactly as context resolution resolves it, so a
// pane whose reported id is gone reports the spend of the session its context
// comes from rather than of a different pane.
func PaneSpend(sessionID, cwd *string, backendID string, startMs, endMs int64) []AssistantMessage {
	backendID = strings.TrimSpace(backendID)
	if backendID == "" {
		return nil
	}
	dbPath := ResolveKiloDBPath()
	if dbPath == "" {
		return nil
	}
	db, err := openReadonlyDB(dbPath)
	if err != nil {
		return nil
	}
	defer db.Close()

	id := resolveSessionIDIn(db, sessionID, cwd)
	if id == "" {
		return nil
	}
	rows, err := db.Query(paneSpendQuery, id, startMs, endMs, backendID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []AssistantMessage
	for rows.Next() {
		var message AssistantMessage
		if err := rows.Scan(&message.Data, &message.TimeCreated); err != nil {
			continue
		}
		out = append(out, message)
	}
	return out
}
