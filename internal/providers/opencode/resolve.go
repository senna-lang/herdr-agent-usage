/**
 * Reads session usage from the OpenCode SQLite database.
 */
package opencode

import (
	"database/sql"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	_ "modernc.org/sqlite"
)

const messageScanLimit = 40

func openReadonlyDB(path string) (*sql.DB, error) {
	// modernc.org/sqlite: file path with mode=ro
	return sql.Open("sqlite", "file:"+path+"?mode=ro")
}

func resolveSessionIDByCwd(db *sql.DB, cwd string) string {
	var id string
	err := db.QueryRow(
		`SELECT id FROM session
		 WHERE directory = ? AND time_archived IS NULL
		 ORDER BY time_updated DESC LIMIT 1`, cwd,
	).Scan(&id)
	if err == nil && id != "" {
		return id
	}
	err = db.QueryRow(
		`SELECT id FROM session
		 WHERE directory LIKE ? AND time_archived IS NULL
		 ORDER BY time_updated DESC LIMIT 1`, cwd+"%",
	).Scan(&id)
	if err == nil && id != "" {
		return id
	}
	return ""
}

func usageForSessionID(db *sql.DB, sessionID string) *core.ContextUsage {
	rows, err := db.Query(
		`SELECT data FROM message
		 WHERE session_id = ?
		 ORDER BY time_updated DESC, time_created DESC
		 LIMIT ?`, sessionID, messageScanLimit,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var jsons []string
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			continue
		}
		jsons = append(jsons, data)
	}
	messageUsage := UsageFromLatestMessageJSONs(jsons)
	if messageUsage == nil {
		return nil
	}
	usage := ToContextUsage(*messageUsage)
	return &usage
}

// ResolveUsageForOpenCode resolves usage from session id and/or cwd.
func ResolveUsageForOpenCode(sessionID, cwd *string) *core.ContextUsage {
	dbPath := ResolveOpenCodeDBPath()
	if dbPath == "" {
		return nil
	}
	db, err := openReadonlyDB(dbPath)
	if err != nil {
		return nil
	}
	defer db.Close()

	id := ""
	if sessionID != nil {
		id = *sessionID
	}
	if id == "" {
		if cwd == nil || *cwd == "" {
			return nil
		}
		id = resolveSessionIDByCwd(db, *cwd)
	}
	if id == "" {
		return nil
	}

	var ok int
	err = db.QueryRow(`SELECT 1 AS ok FROM session WHERE id = ? LIMIT 1`, id).Scan(&ok)
	if err != nil {
		if cwd != nil && *cwd != "" {
			id = resolveSessionIDByCwd(db, *cwd)
			if id == "" {
				return nil
			}
		} else {
			return nil
		}
	}

	return usageForSessionID(db, id)
}
