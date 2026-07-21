/**
 * Claude Code backend detection from deployment env / settings.
 *
 * Claude transcripts do not record the API provider. Official non-Anthropic
 * deployments are selected via env vars (CLAUDE_CODE_USE_BEDROCK, …) or
 * ANTHROPIC_*_BASE_URL — often persisted in settings.json "env".
 *
 * Pure detectors only; file reads live in billingmode_io.go.
 */
package limits

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Default Claude pay-as-you-go label when no cloud/gateway signal is present.
const claudeDefaultBackendID = "anthropic"

// ClaudeBackendFromEnv picks a backend id from Claude Code deployment env.
// Flag-based cloud providers win over bare BASE_URL heuristics.
func ClaudeBackendFromEnv(env map[string]string) string {
	if env == nil {
		return ""
	}
	if envTruthy(env, "CLAUDE_CODE_USE_BEDROCK") || envTruthy(env, "CLAUDE_CODE_USE_MANTLE") {
		return "bedrock"
	}
	if envTruthy(env, "CLAUDE_CODE_USE_VERTEX") {
		return "vertex"
	}
	if envTruthy(env, "CLAUDE_CODE_USE_FOUNDRY") {
		return "foundry"
	}
	// Gateway / custom base without a cloud flag.
	for _, key := range []string{
		"ANTHROPIC_BEDROCK_BASE_URL",
		"ANTHROPIC_BEDROCK_MANTLE_BASE_URL",
		"ANTHROPIC_AWS_BASE_URL",
		"ANTHROPIC_VERTEX_BASE_URL",
		"ANTHROPIC_FOUNDRY_BASE_URL",
		"ANTHROPIC_BASE_URL",
	} {
		if v := strings.TrimSpace(env[key]); v != "" {
			if id := BackendIDFromBaseURL(v); id != "" && id != "anthropic" {
				return id
			}
			// Custom Anthropic-compatible gateway still billed elsewhere.
			if key == "ANTHROPIC_BASE_URL" {
				return "gateway"
			}
			switch key {
			case "ANTHROPIC_BEDROCK_BASE_URL", "ANTHROPIC_BEDROCK_MANTLE_BASE_URL", "ANTHROPIC_AWS_BASE_URL":
				return "bedrock"
			case "ANTHROPIC_VERTEX_BASE_URL":
				return "vertex"
			case "ANTHROPIC_FOUNDRY_BASE_URL":
				return "foundry"
			}
		}
	}
	return ""
}

// ClaudeBillingModeFromEnv returns PayAsYouGo when env evidence shows a
// third-party or gateway deployment; Unknown when there is no signal.
func ClaudeBillingModeFromEnv(env map[string]string) BillingMode {
	if ClaudeBackendFromEnv(env) != "" {
		return BillingPayAsYouGo
	}
	return BillingUnknown
}

// ClaudeEnvFromSettingsJSON extracts the "env" map from a Claude settings
// body. Missing or malformed input yields nil.
func ClaudeEnvFromSettingsJSON(raw string) map[string]string {
	var parsed struct {
		Env map[string]any `json:"env"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || parsed.Env == nil {
		return nil
	}
	out := make(map[string]string, len(parsed.Env))
	for k, v := range parsed.Env {
		switch t := v.(type) {
		case string:
			out[k] = t
		case float64:
			out[k] = strconv.FormatFloat(t, 'f', -1, 64)
		case bool:
			if t {
				out[k] = "1"
			} else {
				out[k] = "0"
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MergeClaudeEnv overlays later maps onto earlier ones (local wins).
func MergeClaudeEnv(layers ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, layer := range layers {
		for k, v := range layer {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func envTruthy(env map[string]string, key string) bool {
	v := strings.TrimSpace(strings.ToLower(env[key]))
	switch v {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// ResolveClaudeBackendID returns the backend label for a Claude pay-as-you-go
// context, falling back to anthropic when no cloud/gateway signal exists.
func ResolveClaudeBackendID(env map[string]string) string {
	if id := ClaudeBackendFromEnv(env); id != "" {
		return id
	}
	return claudeDefaultBackendID
}
