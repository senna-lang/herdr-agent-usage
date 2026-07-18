/**
 * Shared session-ID extraction for usage providers.
 */
package provider

// SessionID returns the herdr agent_session value when it is an ID, else nil.
// Providers whose integration reports kind "id" (codex, opencode, grok) share
// this instead of each keeping an identical private copy.
func SessionID(input UsageResolveInput) *string {
	if input.Session == nil || input.Session.Kind != "id" || input.Session.Value == "" {
		return nil
	}
	v := input.Session.Value
	return &v
}
