package omp

import (
	"os"
	"path/filepath"
	"testing"
)

func writePiModelFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPiContextWindowForModelsStore(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	writePiModelFile(t, filepath.Join(agentDir, "models-store.json"), `{
		"openai-codex": {
			"models": [
				{"id":"gpt-5.6-sol","provider":"openai-codex","contextWindow":272000}
			]
		}
	}`)

	got := PiContextWindowFor("", "openai-codex", "gpt-5.6-sol")
	if got == nil || *got != 272_000 {
		t.Fatalf("got %v", got)
	}
}

func TestPiContextWindowForInfersAgentDirFromSession(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	agentDir := filepath.Join(t.TempDir(), "custom-agent")
	session := filepath.Join(agentDir, "sessions", "--repo--", "session.jsonl")
	writePiModelFile(t, filepath.Join(agentDir, "models-store.json"), `{
		"anthropic": {"models":[{"id":"claude-sonnet-5","contextWindow":1000000}]}
	}`)

	got := PiContextWindowFor(session, "anthropic", "claude-sonnet-5")
	if got == nil || *got != 1_000_000 {
		t.Fatalf("got %v", got)
	}
}

func TestPiContextWindowForModelsJSONOverridesStore(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	writePiModelFile(t, filepath.Join(agentDir, "models-store.json"), `{
		"openai": {"models":[{"id":"gpt-5.6-sol","contextWindow":272000}]}
	}`)
	writePiModelFile(t, filepath.Join(agentDir, "models.json"), `{
		"providers": {
			"openai": {
				"modelOverrides": {
					"gpt-5.6-sol": {"contextWindow":1050000}
				}
			}
		}
	}`)

	got := PiContextWindowFor("", "openai", "gpt-5.6-sol")
	if got == nil || *got != 1_050_000 {
		t.Fatalf("got %v", got)
	}
}

func TestPiContextWindowForCustomModelDefault(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	writePiModelFile(t, filepath.Join(agentDir, "models.json"), `{
		"providers": {
			"ollama": {
				"models": [{"id":"qwen2.5-coder:7b"}]
			}
		}
	}`)

	got := PiContextWindowFor("", "ollama", "qwen2.5-coder:7b")
	if got == nil || *got != piDefaultContextWindow {
		t.Fatalf("got %v", got)
	}
}

func TestResolvePiUsageForPathIncludesModelWindow(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	writePiModelFile(t, filepath.Join(agentDir, "models-store.json"), `{
		"openai-codex": {"models":[{"id":"gpt-5.6-sol","contextWindow":272000}]}
	}`)
	session := filepath.Join(agentDir, "sessions", "--repo--", "session.jsonl")
	writePiModelFile(t, session, `{"type":"session","version":3,"id":"sid","cwd":"/repo"}
{"type":"message","id":"a1","parentId":null,"message":{"role":"assistant","provider":"openai-codex","model":"gpt-5.6-sol","usage":{"totalTokens":125010},"stopReason":"stop"}}
`)

	got := ResolvePiUsageForPath(session)
	if got == nil || got.ContextTokens != 125_010 || got.WindowTokens == nil || *got.WindowTokens != 272_000 {
		t.Fatalf("got %+v", got)
	}
}
