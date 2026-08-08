/**
 * Tests for updates.jsonl live context extraction.
 */
package grok

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLatestContextTokensFromUpdates_LastWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.jsonl")
	content := "" +
		`{"params":{"_meta":{"totalTokens":1000}}}` + "\n" +
		`{"params":{"update":{"sessionUpdate":"agent_message_chunk"}}}` + "\n" +
		`{"params":{"_meta":{"totalTokens":23000},"update":{"sessionUpdate":"tool_call"}}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, mt := LatestContextTokensFromUpdates(path)
	if got != 23_000 {
		t.Fatalf("got %d want 23000", got)
	}
	if mt <= 0 {
		t.Fatal("expected mtime")
	}
}

func TestLatestContextTokensFromUpdates_Missing(t *testing.T) {
	got, mt := LatestContextTokensFromUpdates(filepath.Join(t.TempDir(), "nope.jsonl"))
	if got != 0 || mt != 0 {
		t.Fatalf("got %d %d", got, mt)
	}
}

func TestUsageFromSessionDir_SignalsWinsOverHigherMeta(t *testing.T) {
	dir := t.TempDir()
	// signals is the UI window meter; last totalTokens must not override.
	upd := filepath.Join(dir, "updates.jsonl")
	if err := os.WriteFile(upd, []byte(`{"params":{"_meta":{"totalTokens":210000}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sig := filepath.Join(dir, "signals.json")
	if err := os.WriteFile(sig, []byte(`{"contextTokensUsed":23000,"contextWindowTokens":500000,"totalTokensBeforeCompaction":216407,"compactionCount":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := UsageFromSessionDir(dir)
	if got == nil || got.ContextTokens != 23_000 {
		t.Fatalf("got %+v want signals 23k", got)
	}
}

func TestUsageFromSessionDir_UpdatesWhenSignalsMissing(t *testing.T) {
	dir := t.TempDir()
	upd := filepath.Join(dir, "updates.jsonl")
	if err := os.WriteFile(upd, []byte(`{"params":{"_meta":{"totalTokens":22871}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := UsageFromSessionDir(dir)
	if got == nil || got.ContextTokens != 22_871 {
		t.Fatalf("got %+v want updates 22871", got)
	}
}

func TestUsageFromSessionDir_SignalsNotOverriddenByHigherMeta(t *testing.T) {
	dir := t.TempDir()
	sig := filepath.Join(dir, "signals.json")
	if err := os.WriteFile(sig, []byte(`{"contextTokensUsed":20000,"contextWindowTokens":500000}`), 0o644); err != nil {
		t.Fatal(err)
	}
	upd := filepath.Join(dir, "updates.jsonl")
	if err := os.WriteFile(upd, []byte(`{"params":{"_meta":{"totalTokens":35000}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := UsageFromSessionDir(dir)
	if got == nil || got.ContextTokens != 20_000 {
		t.Fatalf("got %+v want signals 20k (meta must not override)", got)
	}
	if got.WindowTokens == nil || *got.WindowTokens != 500_000 {
		t.Fatalf("should keep window from signals, got %+v", got)
	}
}
