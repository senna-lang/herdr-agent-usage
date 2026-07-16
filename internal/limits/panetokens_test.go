/**
 * Tests for per-provider windowed token aggregation.
 */
package limits

import (
	"encoding/json"
	"testing"
	"time"
)

const (
	windowStart = int64(1_000_000)
	windowEnd   = int64(1_000_000 + 5*60*60*1000)
)

func iso(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
}

func TestSumClaudeTokensInWindow(t *testing.T) {
	lines := []string{
		mustJSON(map[string]any{
			"type":      "assistant",
			"timestamp": iso(windowStart - 1000),
			"message":   map[string]any{"usage": map[string]any{"input_tokens": 999, "output_tokens": 1}},
		}),
		mustJSON(map[string]any{
			"type":      "assistant",
			"timestamp": iso(windowStart + 1000),
			"message": map[string]any{
				"usage": map[string]any{
					"input_tokens":                10,
					"cache_read_input_tokens":     20,
					"cache_creation_input_tokens": 5,
					"output_tokens":               5,
				},
			},
		}),
		mustJSON(map[string]any{
			"type":        "assistant",
			"isSidechain": true,
			"timestamp":   iso(windowStart + 2000),
			"message":     map[string]any{"usage": map[string]any{"input_tokens": 1000}},
		}),
		mustJSON(map[string]any{
			"type":      "user",
			"timestamp": iso(windowStart + 3000),
		}),
	}
	if got := SumClaudeTokensInWindow(lines, windowStart, windowEnd); got != 40 {
		t.Fatalf("got %v want 40", got)
	}
}

func TestSumCodexTokensInWindow_Delta(t *testing.T) {
	lines := []string{
		mustJSON(map[string]any{
			"type":      "event_msg",
			"timestamp": iso(windowStart - 5000),
			"payload": map[string]any{
				"type": "token_count",
				"info": map[string]any{"total_token_usage": map[string]any{"total_tokens": 1000}},
			},
		}),
		mustJSON(map[string]any{
			"type":      "event_msg",
			"timestamp": iso(windowStart + 1000),
			"payload": map[string]any{
				"type": "token_count",
				"info": map[string]any{"total_token_usage": map[string]any{"total_tokens": 1500}},
			},
		}),
		mustJSON(map[string]any{
			"type":      "event_msg",
			"timestamp": iso(windowStart + 2000),
			"payload": map[string]any{
				"type": "token_count",
				"info": map[string]any{"total_token_usage": map[string]any{"total_tokens": 1800}},
			},
		}),
	}
	if got := SumCodexTokensInWindow(lines, windowStart, windowEnd); got != 800 {
		t.Fatalf("got %v want 800", got)
	}
}

func TestSumCodexTokensInWindow_ZeroBaseline(t *testing.T) {
	lines := []string{
		mustJSON(map[string]any{
			"type":      "event_msg",
			"timestamp": iso(windowStart + 1000),
			"payload": map[string]any{
				"type": "token_count",
				"info": map[string]any{"total_token_usage": map[string]any{"total_tokens": 500}},
			},
		}),
	}
	if got := SumCodexTokensInWindow(lines, windowStart, windowEnd); got != 500 {
		t.Fatalf("got %v want 500", got)
	}
}

func TestSumOpenCodeTokensInWindow(t *testing.T) {
	rows := []OpenCodeTokenRow{
		{
			TimeCreated: windowStart + 1,
			Data: mustJSON(map[string]any{
				"role": "assistant",
				"tokens": map[string]any{
					"input": 10, "output": 5, "reasoning": 4,
					"cache": map[string]any{"read": 20, "write": 1},
				},
			}),
		},
		{
			TimeCreated: windowStart - 1,
			Data: mustJSON(map[string]any{
				"role":   "assistant",
				"tokens": map[string]any{"input": 1000},
			}),
		},
		{
			TimeCreated: windowStart + 2,
			Data:        mustJSON(map[string]any{"role": "user", "tokens": map[string]any{"input": 9}}),
		},
	}
	if got := SumOpenCodeTokensInWindow(rows, windowStart, windowEnd); got != 40 {
		t.Fatalf("got %v want 40", got)
	}
}

func TestSumGrokTokensInWindow(t *testing.T) {
	t0 := windowStart / 1000
	lines := []string{
		mustJSON(map[string]any{
			"timestamp": t0 + 1,
			"params": map[string]any{
				"update": map[string]any{
					"sessionUpdate": "turn_completed",
					"usage":         map[string]any{"totalTokens": 100, "inputTokens": 80, "outputTokens": 20},
				},
			},
		}),
		mustJSON(map[string]any{
			"timestamp": t0 + 2,
			"params": map[string]any{
				"update": map[string]any{
					"sessionUpdate": "tool_call",
					"usage":         map[string]any{"totalTokens": 999},
				},
			},
		}),
		mustJSON(map[string]any{
			"timestamp": t0 - 10,
			"params": map[string]any{
				"update": map[string]any{
					"sessionUpdate": "turn_completed",
					"usage":         map[string]any{"totalTokens": 5000},
				},
			},
		}),
		mustJSON(map[string]any{
			"timestamp": t0 + 3,
			"params": map[string]any{
				"update": map[string]any{
					"sessionUpdate": "turn_completed",
					"usage":         map[string]any{"totalTokens": 50},
				},
			},
		}),
	}
	if got := SumGrokTokensInWindow(lines, windowStart, windowEnd); got != 150 {
		t.Fatalf("got %v want 150", got)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
