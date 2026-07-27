/**
 * CredentialType must answer for the provider that was asked about.
 *
 * OpenCode files the Codex OAuth entry under either openai key, so a Codex
 * backend id legitimately reads its sibling. That fallback used to run for
 * every provider id, so asking about anthropic returned the Codex entry's
 * kind — and a subscription route keyed on that answer bound the wrong
 * account. These tests pin the fallback to the openai family.
 */
package opencode

import (
	"os"
	"path/filepath"
	"testing"
)

// codexOnlyAuth is an auth.json holding nothing but the Codex OAuth entry —
// the shape that made an unrelated provider look signed in.
const codexOnlyAuth = `{"openai-codex":{"type":"oauth","refresh":"rt-fixture"}}`

// useOpenCodeAuth points HOME at a fixture tree. An empty authJSON leaves
// auth.json absent.
func useOpenCodeAuth(t *testing.T, authJSON string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if authJSON == "" {
		return
	}
	dir := filepath.Join(home, ".local", "share", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fixture auth dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(authJSON), 0o600); err != nil {
		t.Fatalf("write fixture auth.json: %v", err)
	}
}

func TestCredentialType(t *testing.T) {
	for _, tc := range []struct {
		name     string
		authJSON string
		provider string
		want     string
	}{
		// The regression: the Codex entry answered for a provider that has
		// nothing to do with it.
		{"unrelated provider never reads the codex entry", codexOnlyAuth, "anthropic", ""},
		{"unknown provider never reads the codex entry", codexOnlyAuth, "deepseek", ""},
		{"empty provider id matches nothing", codexOnlyAuth, "", ""},

		// The intended in-family fallback has to survive the fix.
		{"openai falls back to the codex entry", codexOnlyAuth, "openai", "oauth"},
		{"openai-codex reads its own entry", codexOnlyAuth, "openai-codex", "oauth"},
		{"openai-codex-oauth stays in the family", codexOnlyAuth, "openai-codex-oauth", "oauth"},
		{"family fallback tolerates case", codexOnlyAuth, "OpenAI-Codex", "oauth"},

		// An exact entry is always the answer, fallback or not.
		{
			"exact entry beats the family fallback",
			`{"openai":{"type":"api"},"openai-codex":{"type":"oauth"}}`,
			"openai", "api",
		},
		{
			"exact entry answers outside the family",
			`{"anthropic":{"type":"api"},"openai-codex":{"type":"oauth"}}`,
			"anthropic", "api",
		},
		{"kind is normalized", `{"anthropic":{"type":"  OAuth "}}`, "anthropic", "oauth"},

		// Nothing to read is never an error the caller has to handle.
		{"entry carries no type", `{"anthropic":{"refresh":"rt"}}`, "anthropic", ""},
		{"auth.json is unparseable", `{not json`, "openai-codex", ""},
		{"auth.json is absent", "", "openai-codex", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useOpenCodeAuth(t, tc.authJSON)
			if got := CredentialType(tc.provider); got != tc.want {
				t.Fatalf("CredentialType(%q) = %q, want %q", tc.provider, got, tc.want)
			}
		})
	}
}

// The credential value itself must never leave this function: a caller that
// gets a token back would leak it into whatever it renders.
func TestCredentialTypeReturnsKindNotSecret(t *testing.T) {
	useOpenCodeAuth(t, `{"openai-codex":{"type":"oauth","access":"secret-token","refresh":"secret-refresh"}}`)
	if got := CredentialType("openai-codex"); got != "oauth" {
		t.Fatalf("CredentialType = %q, want the kind only", got)
	}
}
