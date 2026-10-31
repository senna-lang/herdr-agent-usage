/**
 * Tests for Devin credential loading and identity.
 *
 * The credential file holds a bearer secret, so the tests pin the two
 * properties that make it safe to handle: a missing or malformed file
 * yields no login, and the account handle derived from a key never contains
 * the key itself.
 */
package devin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCredentials(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCredentials(t *testing.T) {
	path := writeCredentials(t, `
windsurf_api_key = "sk-test-key"
api_server_url = "https://example.test"
`)
	cred := LoadCredentials(path)
	if cred == nil {
		t.Fatal("expected credentials")
	}
	if cred.APIKey != "sk-test-key" {
		t.Fatalf("api key = %q", cred.APIKey)
	}
	if cred.APIServerURL != "https://example.test" {
		t.Fatalf("server url = %q", cred.APIServerURL)
	}
}

func TestLoadCredentials_DefaultsServerURL(t *testing.T) {
	path := writeCredentials(t, `windsurf_api_key = "sk-test-key"`)
	cred := LoadCredentials(path)
	if cred == nil {
		t.Fatal("expected credentials")
	}
	if cred.APIServerURL != DefaultAPIServerURL {
		t.Fatalf("default server url = %q", cred.APIServerURL)
	}
}

func TestLoadCredentials_NoUsableLogin(t *testing.T) {
	if got := LoadCredentials(""); got != nil {
		t.Fatalf("empty path should yield no login, got %+v", got)
	}
	if got := LoadCredentials(filepath.Join(t.TempDir(), "absent.toml")); got != nil {
		t.Fatalf("absent file should yield no login, got %+v", got)
	}
	if got := LoadCredentials(writeCredentials(t, `api_server_url = "https://example.test"`)); got != nil {
		t.Fatalf("keyless file should yield no login, got %+v", got)
	}
	if got := LoadCredentials(writeCredentials(t, `not toml [[[}`)); got != nil {
		t.Fatalf("malformed file should yield no login, got %+v", got)
	}
}

func TestCredentialID_NeverContainsSecret(t *testing.T) {
	id := CredentialID("sk-secret-value")
	if !strings.HasPrefix(id, "devin:") {
		t.Fatalf("id should carry the provider namespace, got %q", id)
	}
	if strings.Contains(id, "sk-secret-value") {
		t.Fatal("id must never embed the key")
	}
	if CredentialID("sk-secret-value") != id {
		t.Fatal("id must be stable for the same key")
	}
	if CredentialID("sk-other") == id {
		t.Fatal("a different login must produce a different id")
	}
}
