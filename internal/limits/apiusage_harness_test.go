/**
 * Tests for the non-OpenCode pay-as-you-go usage decoders.
 *
 * Backend labels for Claude (env/settings) and Grok (modelId+config.toml) are
 * covered in claudebackend_test.go / grokbackend_test.go. The fixtures here
 * exercise the transcript → usage-row decoders used by the panel blocks.
 */
package limits

import "testing"

func TestCodexProviderFromLines_ReadsSessionMeta(t *testing.T) {
	lines := []string{
		`{"type":"session_meta","payload":{"session_id":"x","model_provider":"ollama-launch"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":10}}}}`,
	}
	if got := CodexProviderFromLines(lines); got != "ollama-launch" {
		t.Fatalf("got %q want %q", got, "ollama-launch")
	}
}

func TestCodexProviderFromLines_EmptyWithoutSessionMeta(t *testing.T) {
	lines := []string{`{"type":"event_msg","payload":{"type":"token_count"}}`, "", "junk"}
	if got := CodexProviderFromLines(lines); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestCodexUsageRowsFromLines_EmitsDeltasNotCumulativeTotals(t *testing.T) {
	// total_token_usage is cumulative per session; summing the raw totals
	// (100+250+400) would report 750 instead of the true 400.
	lines := []string{
		`{"type":"event_msg","timestamp":"2027-01-01T00:00:00Z","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":100}}}}`,
		`{"type":"event_msg","timestamp":"2027-01-01T00:10:00Z","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":250}}}}`,
		`{"type":"event_msg","timestamp":"2027-01-01T00:20:00Z","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":400}}}}`,
	}
	rows := CodexUsageRowsFromLines(lines, "gpt-5")
	if len(rows) != 3 {
		t.Fatalf("got %d rows: %#v", len(rows), rows)
	}
	var sum float64
	for _, r := range rows {
		sum += r.Tokens
		if r.ModelID != "gpt-5" {
			t.Fatalf("model not stamped: %#v", r)
		}
	}
	if sum != 400 {
		t.Fatalf("summed %v want 400 (cumulative totals must become deltas)", sum)
	}
}

func TestCodexUsageRowsFromLines_DropsNonPositiveDeltas(t *testing.T) {
	// A compaction can lower the cumulative total; a negative delta must not
	// subtract from the window.
	lines := []string{
		`{"type":"event_msg","timestamp":"2027-01-01T00:00:00Z","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":500}}}}`,
		`{"type":"event_msg","timestamp":"2027-01-01T00:10:00Z","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":200}}}}`,
	}
	rows := CodexUsageRowsFromLines(lines, "")
	if len(rows) != 1 || rows[0].Tokens != 500 {
		t.Fatalf("got %#v want a single 500 row", rows)
	}
}

func TestClaudeUsageRowsFromLines_SumsUsageAndKeepsModel(t *testing.T) {
	lines := []string{
		`{"type":"assistant","timestamp":"2027-01-01T00:00:00Z","message":{"model":"claude-opus-4-8","usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":5,"output_tokens":7}}}`,
		`{"type":"assistant","isSidechain":true,"timestamp":"2027-01-01T00:01:00Z","message":{"model":"claude-opus-4-8","usage":{"input_tokens":999}}}`,
		`{"type":"user","timestamp":"2027-01-01T00:02:00Z"}`,
	}
	rows := ClaudeUsageRowsFromLines(lines)
	if len(rows) != 1 {
		t.Fatalf("sidechain and non-assistant rows must be dropped: %#v", rows)
	}
	if rows[0].Tokens != 42 || rows[0].ModelID != "claude-opus-4-8" {
		t.Fatalf("got %#v want 42 tokens on claude-opus-4-8", rows[0])
	}
}

func TestClaudeUsageRowsFromLines_BlanksSyntheticModel(t *testing.T) {
	lines := []string{
		`{"type":"assistant","timestamp":"2027-01-01T00:00:00Z","message":{"model":"<synthetic>","usage":{"input_tokens":5}}}`,
	}
	rows := ClaudeUsageRowsFromLines(lines)
	if len(rows) != 1 || rows[0].ModelID != "" {
		t.Fatalf("synthetic model must not surface as a model row: %#v", rows)
	}
}

func TestGrokUsageRowsFromLines_ParsesSecondTimestamps(t *testing.T) {
	lines := []string{
		`{"timestamp":1800000000,"params":{"update":{"sessionUpdate":"turn_completed","modelId":"grok-4.5","usage":{"totalTokens":1234}}}}`,
		`{"timestamp":1800000060,"params":{"update":{"sessionUpdate":"turn_started","modelId":"grok-4.5"}}}`,
	}
	rows := GrokUsageRowsFromLines(lines)
	if len(rows) != 1 {
		t.Fatalf("only turn_completed carries usage: %#v", rows)
	}
	if rows[0].Tokens != 1234 || rows[0].ModelID != "grok-4.5" {
		t.Fatalf("got %#v", rows[0])
	}
	// Unix seconds must be scaled to the ms the window helpers expect.
	if rows[0].CreatedMs != 1_800_000_000_000 {
		t.Fatalf("timestamp not converted to ms: %v", rows[0].CreatedMs)
	}
}

func TestCodexRowsFeedSharedWindowHelpers(t *testing.T) {
	// The point of the per-harness decoders: their rows flow through the same
	// windowing and model grouping the OpenCode blocks use.
	rows := []apiUsageRow{
		{CreatedMs: minutesAgo(30), ModelID: "gpt-5", Tokens: 100},
		{CreatedMs: minutesAgo(3 * 24 * 60), ModelID: "gpt-5", Tokens: 200},
	}
	windows := SumAPIWindows(rows, apiNowMs, APIUsageWindowMinutes)
	if windows[0].Tokens != 100 || windows[1].Tokens != 300 {
		t.Fatalf("windows: %#v", windows)
	}
	if AnyAPICost(windows) {
		t.Fatal("harnesses without cost data must report HasCost=false")
	}
	models := SumAPIModels(rows, apiNowMs, APIShareWindowMinutes)
	if len(models) != 1 || models[0].ModelID != "gpt-5" {
		t.Fatalf("models: %#v", models)
	}
}
