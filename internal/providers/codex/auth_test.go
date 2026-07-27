/**
 * Tests for reading the Codex account identity out of auth.json.
 */
package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCodexAuth(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestAccountID(t *testing.T) {
	// The real shape: the id sits under tokens, beside secrets this must not
	// touch.
	const real = `{
		"last_refresh": "2026-07-25T12:30:49Z",
		"OPENAI_API_KEY": null,
		"tokens": {
			"access_token": "secret-access",
			"account_id": "a1afefbc-024d-46be-beb3-add8311de670",
			"id_token": "secret-id",
			"refresh_token": "secret-refresh"
		}
	}`

	cases := []struct {
		name string
		body string
		want string
	}{
		{"real auth.json", real, "a1afefbc-024d-46be-beb3-add8311de670"},
		{"surrounding whitespace trimmed", `{"tokens":{"account_id":"  abc-123  "}}`, "abc-123"},
		{"api-key login has no account id", `{"OPENAI_API_KEY":"sk-x","tokens":null}`, ""},
		{"tokens without account_id", `{"tokens":{"access_token":"x"}}`, ""},
		{"account_id is null", `{"tokens":{"account_id":null}}`, ""},
		{"empty object", `{}`, ""},
		{"unparseable json", `{"tokens":`, ""},
		{"empty file", ``, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEX_HOME", writeCodexAuth(t, tc.body))
			if got := AccountID(); got != tc.want {
				t.Fatalf("AccountID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAccountIDWithoutAuthFile(t *testing.T) {
	// Never signed in here: the caller must get "" so a borrowed rate-limit
	// window falls back to the pool's own single-account rule instead of
	// being matched against a fabricated identity.
	t.Setenv("CODEX_HOME", t.TempDir())
	if got := AccountID(); got != "" {
		t.Fatalf("AccountID() = %q, want empty", got)
	}
}

func TestAccountIDNeverReturnsSecrets(t *testing.T) {
	t.Setenv("CODEX_HOME", writeCodexAuth(t, `{
		"tokens": {
			"access_token": "sk-super-secret",
			"id_token": "jwt-secret",
			"refresh_token": "rt-secret",
			"account_id": "acct-1"
		}
	}`))
	got := AccountID()
	if got != "acct-1" {
		t.Fatalf("AccountID() = %q, want %q", got, "acct-1")
	}
	for _, secret := range []string{"sk-super-secret", "jwt-secret", "rt-secret"} {
		if got == secret {
			t.Fatalf("AccountID() leaked %q", secret)
		}
	}
}
