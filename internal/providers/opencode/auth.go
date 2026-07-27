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
	// OpenCode files the Codex OAuth entry under either openai key, so a Codex
	// backend id may have to look at its sibling. Probing those keys for an
	// unrelated provider (anthropic, …) would answer with a credential that
	// has nothing to do with the queried account — and a route keyed on that
	// answer binds the wrong subscription. The fallback stays in the family.
	candidates := []string{providerID}
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "openai", "openai-codex", "openai-codex-oauth":
		candidates = append(candidates, "openai-codex", "openai")
	}
	for _, id := range candidates {
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
