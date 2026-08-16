/**
 * Reads only the account identity from Codex's auth.json.
 *
 * The tokens themselves are never touched. The account id exists here for one
 * purpose: a rate-limit window borrowed from another agent must be checkable
 * against the account this machine's Codex CLI is actually signed into, so a
 * second account's numbers can never be shown on a Codex pane.
 */
package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// AccountID returns the ChatGPT account id recorded in $CODEX_HOME/auth.json,
// or "" when Codex has never signed in on this machine — in which case there
// is no local identity to check a borrowed observation against.
func AccountID() string {
	return AccountIDIn(codexHome())
}

// AccountIDIn reads auth.json under the given Codex home.
func AccountIDIn(home string) string {
	if home == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		return ""
	}
	var parsed struct {
		Tokens *struct {
			AccountID *string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &parsed) != nil || parsed.Tokens == nil || parsed.Tokens.AccountID == nil {
		return ""
	}
	return strings.TrimSpace(*parsed.Tokens.AccountID)
}
