/**
 * Tests for Kilo's credential reader.
 *
 * The property under test is attribution: only the gateway device login may
 * stand behind a reading, and a gateway API key never may, because it bills the
 * same account while naming none.
 */
package kilo

import "testing"

const storeWithBothKinds = `{
	"kilo": {"type":"oauth","refresh":"rt_secret","access":"st_access","expires":999},
	"opencode-go": {"type":"api","key":"og_secret"},
	"openrouter": {"type":"api"}
}`

// The same login with a team selected. Kilo files the selected organization in
// the login's accountId, and that scope decides whose wallet is read.
const storeScopedToATeam = `{
	"kilo": {"type":"oauth","refresh":"rt_secret","access":"st_access","accountId":"team_9f2"},
	"opencode-go": {"type":"api","key":"og_secret"}
}`

func TestCredentialType_ReportsTheKindOnly(t *testing.T) {
	auth := ParseAuthJSON([]byte(storeWithBothKinds))
	if got := CredentialTypeIn(auth, "kilo"); got != "oauth" {
		t.Fatalf("kilo kind = %q", got)
	}
	if got := CredentialTypeIn(auth, "opencode-go"); got != "api" {
		t.Fatalf("opencode-go kind = %q", got)
	}
	if got := CredentialTypeIn(auth, "absent"); got != "" {
		t.Fatalf("absent provider = %q, want empty", got)
	}
}

func TestCredentialType_DoesNotLeakSecrets(t *testing.T) {
	// The kind is the whole contract: a caller that could read the value would
	// start using it for attribution, which is exactly what must not happen.
	auth := ParseAuthJSON([]byte(storeWithBothKinds))
	if got := CredentialTypeIn(auth, "kilo"); len(got) > 0 && (got == "st_access" || got == "rt_secret") {
		t.Fatalf("credential kind returned secret material: %q", got)
	}
}

func TestGatewayLogin_AcceptsOnlyTheDeviceLogin(t *testing.T) {
	// A gateway API key bills the same account but cannot name it, so it is
	// never the attribution for a reading.
	for name, store := range map[string]string{
		"api key":        `{"kilo":{"type":"api","key":"sk-gateway"}}`,
		"blank access":   `{"kilo":{"type":"oauth","refresh":"rt","access":"   "}}`,
		"no access":      `{"kilo":{"type":"oauth","refresh":"rt"}}`,
		"wrong type":     `{"kilo":{"type":"wellknown","key":"sk"}}`,
		"other provider": `{"opencode-go":{"type":"oauth","access":"og"}}`,
		"not json":       `{not json`,
	} {
		auth := ParseAuthJSON([]byte(store))
		if got := GatewayLoginIn(auth); got != nil {
			t.Fatalf("%s: accepted a credential it must refuse: %+v", name, got.Identity)
		}
	}
}

func TestGatewayLogin_ReadsTheDeviceLoginAndStampsAnIdentity(t *testing.T) {
	auth := ParseAuthJSON([]byte(storeWithBothKinds))
	got := GatewayLoginIn(auth)
	if got == nil {
		t.Fatal("gateway device login was refused")
	}
	if got.Access != "st_access" {
		t.Fatalf("access = %q", got.Access)
	}
	// The identity is a hash, so it can key a cache without the token ever
	// reaching disk.
	if got.Identity == "" || got.Identity == got.Access {
		t.Fatalf("account identity is not an opaque hash: %q", got.Identity)
	}
	// No accountId means personal scope, which is the scope a Kilo Pass belongs
	// to. A reader that treated the blank as a named organization would refuse
	// every personal login.
	if got.OrganizationID != "" {
		t.Fatalf("organization = %q, want none for a personal-scope login", got.OrganizationID)
	}
}

func TestGatewayLogin_CarriesTheSelectedOrganization(t *testing.T) {
	// A team-billed pane fetches that team's wallet, so the scope has to survive
	// the credential read rather than being re-guessed at the call site.
	got := GatewayLoginIn(ParseAuthJSON([]byte(storeScopedToATeam)))
	if got == nil {
		t.Fatal("team-scoped device login was refused")
	}
	if got.OrganizationID != "team_9f2" {
		t.Fatalf("organization = %q", got.OrganizationID)
	}
	if got.Identity == AccountIDForToken(got.Access, "") {
		t.Fatal("an organization-scoped login shares the personal scope's identity")
	}
}

func TestGatewayLogin_DifferentOrganizationsGetDifferentIdentities(t *testing.T) {
	// Switching teams keeps the same token. Keying the cache on the token alone
	// would serve the previous organization's window for the new one.
	first := GatewayLoginIn(ParseAuthJSON([]byte(`{"kilo":{"type":"oauth","access":"tok","accountId":"team_a"}}`)))
	second := GatewayLoginIn(ParseAuthJSON([]byte(`{"kilo":{"type":"oauth","access":"tok","accountId":"team_b"}}`)))
	if first == nil || second == nil {
		t.Fatal("expected both scopes to resolve")
	}
	if first.Identity == second.Identity {
		t.Fatalf("two organizations share one identity: %q", first.Identity)
	}
	if first.Identity != AccountIDForToken("tok", "team_a") {
		t.Fatalf("identity is not stable for one scope: %q", first.Identity)
	}
}

func TestGatewayLogin_DifferentLoginsGetDifferentIdentities(t *testing.T) {
	// This is what makes a cached reading refusable: another login must produce
	// a different identity, or one account's numbers could answer for another.
	first := GatewayLoginIn(ParseAuthJSON([]byte(`{"kilo":{"type":"oauth","access":"tok_a"}}`)))
	second := GatewayLoginIn(ParseAuthJSON([]byte(`{"kilo":{"type":"oauth","access":"tok_b"}}`)))
	if first == nil || second == nil {
		t.Fatal("expected both logins to resolve")
	}
	if first.Identity == second.Identity {
		t.Fatalf("two logins share one identity: %q", first.Identity)
	}
	if first.Identity != AccountIDForToken("tok_a", "") {
		t.Fatalf("identity is not stable for one login: %q", first.Identity)
	}
}

func TestIsGatewayProvider(t *testing.T) {
	for _, id := range []string{"kilo", "KILO", " kilo "} {
		if !IsGatewayProvider(id) {
			t.Fatalf("%q should be the gateway", id)
		}
	}
	for _, id := range []string{"opencode-go", "openrouter", "", "kilocode"} {
		if IsGatewayProvider(id) {
			t.Fatalf("%q should not be the gateway", id)
		}
	}
}

func TestParseAuthJSON_SkipsUnparsableEntries(t *testing.T) {
	// One bad entry must not discard the good ones beside it.
	auth := ParseAuthJSON([]byte(`{"kilo":"not-an-object","opencode-go":{"type":"api","key":"x"}}`))
	if got := CredentialTypeIn(auth, "kilo"); got != "" {
		t.Fatalf("unparsable entry produced a kind: %q", got)
	}
	if got := CredentialTypeIn(auth, "opencode-go"); got != "api" {
		t.Fatalf("sibling entry lost: %q", got)
	}
}
