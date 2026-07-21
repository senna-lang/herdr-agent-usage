/**
 * Derives a short backend id from an inference base URL.
 *
 * Used when a harness records (or can be joined to) a URL but not a
 * providerID — Grok custom models, Claude gateways, etc.
 */
package limits

import (
	"net/url"
	"strings"
)

// BackendIDFromBaseURL maps an inference endpoint URL to a short backend id
// ("openai", "ollama", "xai"). Empty input yields "".
func BackendIDFromBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	port := u.Port()

	switch {
	case host == "api.x.ai" || host == "x.ai" || strings.HasSuffix(host, ".x.ai") ||
		strings.Contains(host, "grok.com") || strings.Contains(host, "spacexai.com"):
		return "xai"
	case host == "api.anthropic.com" || host == "anthropic.com" || strings.HasSuffix(host, ".anthropic.com"):
		return "anthropic"
	case host == "api.openai.com" || host == "openai.com" || strings.HasSuffix(host, ".openai.com"):
		return "openai"
	case strings.Contains(host, "together.xyz") || strings.Contains(host, "together.ai"):
		return "together"
	case strings.Contains(host, "deepseek"):
		return "deepseek"
	case strings.Contains(host, "openrouter"):
		return "openrouter"
	case strings.Contains(host, "bedrock") || strings.Contains(host, "amazonaws.com"):
		return "bedrock"
	case strings.Contains(host, "googleapis.com") || strings.Contains(host, "vertexai") ||
		strings.Contains(host, "aiplatform"):
		return "vertex"
	case strings.Contains(host, "azure") || strings.Contains(host, "foundry"):
		return "foundry"
	case host == "localhost" || host == "127.0.0.1" || host == "::1":
		if port == "11434" {
			return "ollama"
		}
		return "local"
	}

	// api.foo.bar -> foo; foo.bar -> foo
	parts := strings.Split(host, ".")
	if len(parts) >= 2 && (parts[0] == "api" || parts[0] == "www" || parts[0] == "inference") {
		return sanitizeBackendID(parts[1])
	}
	return sanitizeBackendID(parts[0])
}

func sanitizeBackendID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return -1
	}, s)
	return s
}

// IsXAIBaseURL reports whether the URL points at an xAI-hosted endpoint.
func IsXAIBaseURL(raw string) bool {
	return BackendIDFromBaseURL(raw) == "xai"
}
