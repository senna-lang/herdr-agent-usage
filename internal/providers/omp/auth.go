package omp

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// CredentialType returns the active OMP credential kind for a provider
// without reading its secret material. OMP records OAuth and API-key
// credentials separately in agent.db.
func CredentialType(provider string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(home, ".omp", "agent", "agent.db")+"?mode=ro")
	if err != nil {
		return ""
	}
	defer db.Close()
	var kind string
	err = db.QueryRow(`SELECT credential_type FROM auth_credentials WHERE provider = ? AND disabled_cause IS NULL ORDER BY updated_at DESC LIMIT 1`, provider).Scan(&kind)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(kind))
}

// PiCredentialType reads only the credential kind from Pi's auth.json. Pi
// stores provider entries as JSON objects; tokens are intentionally ignored.
func PiCredentialType(provider string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "auth.json"))
	if err != nil {
		return ""
	}
	var entries map[string]map[string]any
	if json.Unmarshal(raw, &entries) != nil {
		return ""
	}
	entry := entries[provider]
	for _, key := range []string{"type", "credential_type", "auth_type"} {
		if value, ok := entry[key].(string); ok {
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	return ""
}
