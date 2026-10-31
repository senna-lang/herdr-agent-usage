/**
 * Tests for the Devin session store reads.
 *
 * The fixtures mirror the rows Devin CLI actually writes: one sessions row
 * per conversation and a forest of message_nodes, where several sibling
 * nodes share a request_id and repeat its metrics — Devin writes one node
 * per streamed update of the same request, so any raw sum multiplies usage.
 */
package devin

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeStore builds a Devin data directory holding cli/sessions.db with the
// given sessions, each populated by its own node rows.
func writeStore(t *testing.T, sessions []storeSession) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "cli", "sessions.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, ddl := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, working_directory TEXT, last_activity_at INTEGER, title TEXT)`,
		`CREATE TABLE message_nodes (row_id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, node_id INTEGER NOT NULL, chat_message TEXT NOT NULL, created_at INTEGER NOT NULL)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range sessions {
		if _, err := db.Exec(`INSERT INTO sessions (id, working_directory, last_activity_at, title) VALUES (?,?,?,?)`,
			s.id, "/repo", s.lastActivitySec, s.id); err != nil {
			t.Fatal(err)
		}
		for i, node := range s.nodes {
			if _, err := db.Exec(`INSERT INTO message_nodes (session_id, node_id, chat_message, created_at) VALUES (?,?,?,?)`,
				s.id, i, node.message, node.createdAtSec); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

// storeSession is one sessions row and the node rows beneath it.
type storeSession struct {
	id              string
	lastActivitySec int64
	nodes           []storeNode
}

type storeNode struct {
	createdAtSec int64
	message      string
}

// requestNode renders one assistant node the way Devin writes it: metrics
// under metadata.metrics, ownership under metadata.request_id.
func requestNode(requestID string, createdAtSec int64, metrics string) storeNode {
	return storeNode{
		createdAtSec: createdAtSec,
		message:      fmt.Sprintf(`{"role":"assistant","metadata":{"request_id":%q,"generation_model":"swe-2-max","metrics":{%s}}}`, requestID, metrics),
	}
}

// streamNode duplicates one request's metrics the way a streamed update
// does: same request_id, later row, repeated counters.
func streamNode(requestID string, createdAtSec int64, metrics string) storeNode {
	return requestNode(requestID, createdAtSec, metrics)
}

func userNode(createdAtSec int64) storeNode {
	return storeNode{createdAtSec: createdAtSec, message: `{"role":"user","content":"hi"}`}
}

const oneRequest = `"context_input_tokens":1000,"input_tokens":800,"output_tokens":120,"cache_read_tokens":600,"cache_creation_tokens":40`

func TestResolveUsageIn_DedupesStreamedRequests(t *testing.T) {
	dir := writeStore(t, []storeSession{{
		id:              "pane-a",
		lastActivitySec: 200,
		nodes: []storeNode{
			userNode(90),
			requestNode("req-1", 100, oneRequest),
			streamNode("req-1", 101, oneRequest), // same request, later row
			requestNode("req-2", 150, `"context_input_tokens":2200,"input_tokens":1900,"output_tokens":300,"cache_read_tokens":1500`),
		},
	}})

	usage := ResolveUsageIn(dir, "pane-a")
	if usage == nil {
		t.Fatal("expected usage")
	}
	// Latest request's context, not the sum of both.
	if usage.ContextTokens != 2200 {
		t.Fatalf("context = %d", usage.ContextTokens)
	}
	// Lifetime cache input: req-1 counted once (800 fresh + 600 read + 40
	// create) plus req-2 (1900 + 1500). Doubling req-1 would prove the
	// streamed sibling leaked into the aggregate.
	if usage.SessionCache == nil {
		t.Fatal("expected session cache")
	}
	if usage.SessionCache.FreshInputTokens != 800+1900 ||
		usage.SessionCache.ReadTokens != 600+1500 ||
		usage.SessionCache.CreationTokens != 40 {
		t.Fatalf("session cache = %+v", usage.SessionCache)
	}
}

func TestResolveUsageIn_LatestMetricsFollowRowOrder(t *testing.T) {
	// node_id ordering is unreliable across tree branches; recency must come
	// from insertion order, so the older-created row inserted last wins.
	dir := writeStore(t, []storeSession{{
		id:              "pane-a",
		lastActivitySec: 200,
		nodes: []storeNode{
			requestNode("req-1", 150, `"context_input_tokens":9000,"input_tokens":8000,"output_tokens":10`),
			requestNode("req-2", 100, `"context_input_tokens":500,"input_tokens":400,"output_tokens":10`),
		},
	}})
	usage := ResolveUsageIn(dir, "pane-a")
	if usage == nil {
		t.Fatal("expected usage")
	}
	if usage.ContextTokens != 500 {
		t.Fatalf("context should follow the newest row, got %d", usage.ContextTokens)
	}
}

func TestResolveUsageIn_UnknownAndMetriclessSessions(t *testing.T) {
	dir := writeStore(t, []storeSession{
		{id: "empty", lastActivitySec: 10, nodes: []storeNode{userNode(5)}},
	})
	if got := ResolveUsageIn(dir, "absent-session"); got != nil {
		t.Fatalf("unknown session should yield no usage, got %+v", got)
	}
	usage := ResolveUsageIn(dir, "empty")
	if usage == nil {
		t.Fatal("a real session with no assistant rows still resolves")
	}
	if !usage.ContextUnavailable {
		t.Fatal("no metrics rows means context is unavailable, not zero")
	}
	if usage.ContextTokens != 0 {
		t.Fatalf("context = %d", usage.ContextTokens)
	}
	if got := ResolveUsageIn("", "empty"); got != nil {
		t.Fatalf("no data dir should yield no usage, got %+v", got)
	}
}

func TestSessionTokensInWindow(t *testing.T) {
	dir := writeStore(t, []storeSession{{
		id:              "pane-a",
		lastActivitySec: 1000,
		nodes: []storeNode{
			requestNode("req-old", 50, `"input_tokens":100,"output_tokens":10`),
			requestNode("req-in", 500, `"input_tokens":300,"output_tokens":20,"cache_read_tokens":900`),
			streamNode("req-in", 501, `"input_tokens":300,"output_tokens":20,"cache_read_tokens":900`),
			requestNode("req-late", 2000, `"input_tokens":700,"output_tokens":70`),
		},
	}})
	// Window covers only req-in: fresh input+output, cache reads excluded,
	// streamed sibling counted once.
	got := SessionTokensInWindow(dir, "pane-a", 400_000, 1_500_000)
	if got != 320 {
		t.Fatalf("windowed tokens = %v", got)
	}
	if got := SessionTokensInWindow(dir, "pane-a", 0, 100_000); got != 110 {
		t.Fatalf("early window = %v", got)
	}
	if got := SessionTokensInWindow(dir, "", 0, 1_500_000); got != 0 {
		t.Fatalf("empty session id should yield 0, got %v", got)
	}
	if got := SessionTokensInWindow(dir, "absent", 0, 1_500_000); got != 0 {
		t.Fatalf("unknown session should yield 0, got %v", got)
	}
}

func TestTotalTokensInWindow_SkipsIdleSessions(t *testing.T) {
	dir := writeStore(t, []storeSession{
		{
			id:              "active",
			lastActivitySec: 500,
			nodes:           []storeNode{requestNode("req-1", 500, `"input_tokens":300,"output_tokens":20`)},
		},
		{
			id:              "idle",
			lastActivitySec: 10,
			nodes:           []storeNode{requestNode("req-2", 10, `"input_tokens":900,"output_tokens":90`)},
		},
	})
	got := TotalTokensInWindow(dir, 400_000, 1_500_000)
	if got != 320 {
		t.Fatalf("total should only count the active session, got %v", got)
	}
	if got := TotalTokensInWindow(dir, 0, 1_500_000); got != 320+990 {
		t.Fatalf("full range = %v", got)
	}
}
