/**
 * Reads Devin CLI's credential store.
 *
 * `devin login` writes credentials.toml with a Windsurf-family API key and
 * the seat-management endpoint it belongs to. The key is a bearer secret:
 * it is only ever sent to the recorded api_server_url (default
 * https://server.codeium.com), never logged, and never persisted by this
 * plugin — callers that need an account handle use CredentialID, which is
 * a one-way hash.
 */
package devin

import (
	"crypto/sha256"
	"encoding/hex"
	"os"

	"github.com/BurntSushi/toml"
)

// DefaultAPIServerURL is where Devin accounts live when credentials.toml
// names no explicit server.
const DefaultAPIServerURL = "https://server.codeium.com"

// Credentials is the login Devin CLI recorded for this machine.
type Credentials struct {
	// APIKey is the bearer credential for seat-management requests.
	APIKey string
	// APIServerURL is the endpoint the key was issued against; defaults to
	// DefaultAPIServerURL when the file leaves it unset.
	APIServerURL string
}

type credentialsFile struct {
	WindsurfAPIKey string `toml:"windsurf_api_key"`
	APIServerURL   string `toml:"api_server_url"`
}

// LoadCredentials reads one credentials.toml. A nil result means there is no
// usable login: the file is absent, unreadable, malformed, or has no key.
func LoadCredentials(path string) *Credentials {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var parsed credentialsFile
	if err := toml.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	if parsed.WindsurfAPIKey == "" {
		return nil
	}
	server := parsed.APIServerURL
	if server == "" {
		server = DefaultAPIServerURL
	}
	return &Credentials{APIKey: parsed.WindsurfAPIKey, APIServerURL: server}
}

// CredentialID returns a stable, non-reversible identifier for an API key,
// used to scope cached readings. The key is hashed rather than stored: a
// cached limit row needs to know *which account* produced it, and that must
// never require keeping the secret itself on disk. A different login
// produces a different id, which is exactly what makes another account's
// cached reading refuseable.
func CredentialID(apiKey string) string {
	sum := sha256.Sum256([]byte("devin\000" + apiKey))
	return "devin:" + hex.EncodeToString(sum[:])
}
