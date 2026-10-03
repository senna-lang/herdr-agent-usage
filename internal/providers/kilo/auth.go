/**
 * Kilo's credential store: ~/.local/share/kilo/auth.json.
 *
 * The file holds one entry per provider id, and the entry's *type* is what
 * matters for attribution. Two kinds exist here and they are not equivalent:
 *
 *   {"type":"oauth", "access": …, "refresh": …, "accountId": …}   the Kilo
 *       Gateway device login. Its allowance is the account's, and it is the
 *       only credential that can name the account. Its accountId is the
 *       selected team, empty for a personal-scope login, and it decides whose
 *       wallet the balance request reads.
 *
 *   {"type":"api", "key": …}                       a gateway API key. It bills
 *       the same account but says nothing about *which* account, so it can
 *       never be the attribution for a reading.
 *
 * CredentialType returns the kind only and never the value, following the same
 * rule the OpenCode and OMP readers use. The gateway login itself is read only
 * by the collector that must authenticate an HTTP request, it is held in
 * memory for that request alone, and the refresh token is never read at all.
 */
package kilo

import (
	"encoding/json"
	"os"
	"strings"
)

// GatewayProviderID is Kilo's own provider id for the Kilo Gateway, the only
// backend whose sessions draw on a Kilo allowance.
const GatewayProviderID = "kilo"

// IsGatewayProvider reports whether a backend id is the Kilo Gateway.
func IsGatewayProvider(providerID string) bool {
	return strings.EqualFold(strings.TrimSpace(providerID), GatewayProviderID)
}

// CredentialKind is the type recorded for one provider, with no secret
// material attached.
type CredentialKind struct {
	ProviderID string
	// Kind is "oauth", "api", or "" when the entry is absent or unrecognised.
	Kind string
	// HasSecret records that a value is present, without exposing it.
	HasSecret bool
}

// CredentialType returns only the authentication kind stored for a Kilo
// provider id, or "" when the store has no usable entry for it.
//
// Probing a sibling provider's key would answer with a credential that has
// nothing to do with the queried account, and a route keyed on that answer
// binds the wrong subscription — so only the exact id is read.
func CredentialType(providerID string) string {
	auth := readAuthMap(ResolveKiloAuthPath())
	if auth == nil {
		return ""
	}
	return CredentialTypeIn(auth, providerID)
}

// CredentialTypeIn is CredentialType over an already-parsed store.
func CredentialTypeIn(auth map[string]map[string]any, providerID string) string {
	entry, ok := auth[strings.ToLower(strings.TrimSpace(providerID))]
	if !ok {
		return ""
	}
	kind, _ := entry["type"].(string)
	return kind
}

// GatewayCredential is the one credential the Kilo collector may send.
//
// The access token is used for a single HTTP request and never persisted. The
// OrganizationID is Kilo's selected team, and Identity is a hash of the token
// together with that scope, so a cached reading can be refused for another
// login or another organization without either secret ever reaching disk.
type GatewayCredential struct {
	Identity string
	// OrganizationID is empty for a personal-scope login. A team-scoped login
	// bills that team's wallet and never carries the person's Kilo Pass.
	OrganizationID string
	Access         string
}

// GatewayLogin reads the Kilo Gateway device login, or nil when the store has
// none.
//
// A gateway API key is deliberately not accepted here: it bills the same
// account but cannot name it, so attributing a reading to it would show one
// account's allowance for another account's pane.
func GatewayLogin(authPath string) *GatewayCredential {
	return GatewayLoginIn(readAuthMap(authPath))
}

// GatewayLoginIn is GatewayLogin over an already-parsed store.
func GatewayLoginIn(auth map[string]map[string]any) *GatewayCredential {
	entry, ok := auth[GatewayProviderID]
	if !ok {
		return nil
	}
	if kind, _ := entry["type"].(string); kind != "oauth" {
		return nil
	}
	access := asString(entry["access"])
	if access == "" {
		return nil
	}
	// Kilo files the selected team in the login's accountId, and that scope
	// decides whose wallet the balance request reads. It is part of the billing
	// identity, not a detail of one request: a cache keyed on the token alone
	// would hand the previous scope's numbers to the newly selected one.
	organization := asString(entry["accountId"])
	return &GatewayCredential{
		Identity:       AccountIDForToken(access, organization),
		OrganizationID: organization,
		Access:         access,
	}
}

// readAuthMap parses Kilo's auth.json into provider id -> entry.
//
// Values are retained because the collector needs the gateway access token to
// authenticate its request; nothing else in this package reads them, and the
// parsed map is never logged or returned to a caller.
func readAuthMap(path string) map[string]map[string]any {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return ParseAuthJSON(raw)
}

// ParseAuthJSON parses Kilo's auth.json. Exported so tests and the limits
// collector can exercise parsing without touching the filesystem.
func ParseAuthJSON(raw []byte) map[string]map[string]any {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}
	out := make(map[string]map[string]any, len(root))
	for id, blob := range root {
		var entry map[string]any
		if err := json.Unmarshal(blob, &entry); err != nil {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(id))] = entry
	}
	return out
}

// AccountIDForToken is the opaque identity stamped on a cached reading.
//
// It is a hash of the access token together with the selected organization, so
// it changes when either does. Kilo issues long-lived device logins, so in
// practice this is stable for the life of a login and one scope; when it
// changes, the next refresh simply re-reads rather than reusing the previous
// scope's numbers.
func AccountIDForToken(access, organizationID string) string {
	return CredentialID(access + "\x00" + organizationID)
}
