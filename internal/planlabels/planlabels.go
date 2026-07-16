/**
 * Normalizes raw provider plan identifiers into short UI-friendly labels.
 */
package planlabels

import (
	"strings"
	"unicode"
)

// ClaudePlanLabel maps oauthAccount.organizationType / rate_limit_tier, etc.
func ClaudePlanLabel(organizationType *string, rateLimitTier *string) *string {
	org := ""
	if organizationType != nil {
		org = strings.ToLower(*organizationType)
	}
	switch {
	case strings.Contains(org, "max") || org == "claude_max":
		return strPtr("Max")
	case strings.Contains(org, "team") || org == "claude_team":
		return strPtr("Team")
	case strings.Contains(org, "enterprise") || org == "claude_enterprise":
		return strPtr("Enterprise")
	case strings.Contains(org, "pro") || org == "claude_pro":
		return strPtr("Pro")
	case strings.Contains(org, "free") || org == "claude_free":
		return strPtr("Free")
	}

	tier := ""
	if rateLimitTier != nil {
		tier = strings.ToLower(*rateLimitTier)
	}
	switch {
	case strings.Contains(tier, "max_20") || strings.Contains(tier, "20x"):
		return strPtr("Max 20x")
	case strings.Contains(tier, "max_5") || strings.Contains(tier, "5x"):
		return strPtr("Max 5x")
	case strings.Contains(tier, "max"):
		return strPtr("Max")
	case strings.Contains(tier, "pro"):
		return strPtr("Pro")
	case strings.Contains(tier, "team"):
		return strPtr("Team")
	case strings.Contains(tier, "enterprise"):
		return strPtr("Enterprise")
	}

	if organizationType != nil && len(*organizationType) > 0 {
		return strPtr(humanizeToken(*organizationType))
	}
	return nil
}

// GrokPlanLabel maps rest/subscriptions `tier` field.
// Example: SUBSCRIPTION_TIER_SUPER_GROK_LITE -> Lite
func GrokPlanLabel(tier *string) *string {
	if tier == nil || len(*tier) == 0 {
		return nil
	}
	t := strings.ToUpper(*tier)
	switch {
	case strings.Contains(t, "LITE"):
		return strPtr("Lite")
	case strings.Contains(t, "HEAVY") || strings.Contains(t, "ULTRA"):
		return strPtr("Heavy")
	case strings.Contains(t, "SUPER_GROK") || t == "SUBSCRIPTION_TIER_SUPER_GROK":
		return strPtr("SuperGrok")
	case strings.Contains(t, "FREE"):
		return strPtr("Free")
	}
	stripped := strings.TrimPrefix(t, "SUBSCRIPTION_TIER_")
	stripped = strings.TrimPrefix(stripped, "SUBSCRIPTION_STATUS_")
	return strPtr(humanizeToken(stripped))
}

// CodexPlanLabel maps Codex rollout plan_type.
func CodexPlanLabel(planType *string) *string {
	if planType == nil || len(*planType) == 0 {
		return nil
	}
	p := strings.ToLower(*planType)
	switch p {
	case "plus":
		return strPtr("Plus")
	case "pro":
		return strPtr("Pro")
	case "team":
		return strPtr("Team")
	case "enterprise":
		return strPtr("Enterprise")
	case "free":
		return strPtr("Free")
	default:
		return strPtr(humanizeToken(*planType))
	}
}

// OpencodePlanLabel maps OpenCode Go plan identifiers, etc.
func OpencodePlanLabel(plan *string) *string {
	if plan == nil || len(*plan) == 0 {
		return nil
	}
	if strings.ToLower(*plan) == "go" {
		return strPtr("Go")
	}
	return strPtr(humanizeToken(*plan))
}

func humanizeToken(raw string) string {
	s := raw
	if strings.HasPrefix(strings.ToLower(s), "claude_") {
		s = s[len("claude_"):]
	}
	// replace [_-]+ with space
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == '_' || r == '-' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	parts := strings.Fields(strings.TrimSpace(b.String()))
	for i, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		for j := 1; j < len(runes); j++ {
			runes[j] = unicode.ToLower(runes[j])
		}
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func strPtr(s string) *string { return &s }
