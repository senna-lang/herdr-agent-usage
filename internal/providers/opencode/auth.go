package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// CredentialType returns only the authentication kind stored for an OpenCode
// provider (oauth, api, …), never the credential value itself.
func CredentialType(providerID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(home, ".local", "share", "opencode", "auth.json"))
	if err != nil {
		return ""
	}
	var entries map[string]map[string]any
	if json.Unmarshal(raw, &entries) != nil {
		return ""
	}
	for _, id := range []string{providerID, "openai-codex", "openai"} {
		entry, ok := entries[id]
		if !ok {
			continue
		}
		if kind, ok := entry["type"].(string); ok {
			return strings.ToLower(strings.TrimSpace(kind))
		}
	}
	return ""
}
