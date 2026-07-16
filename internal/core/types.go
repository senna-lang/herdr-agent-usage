/**
 * Shared types for the core layer. Provider-specific usage shapes do not belong here.
 */
package core

// ContextUsage is the minimum usage information required for the sidebar display.
// Token aggregation and model-window resolution must already be done by each
// provider; this type only carries the final result.
type ContextUsage struct {
	// ContextTokens is the current context-occupying token count (already aggregated).
	ContextTokens int
	// WindowTokens is the context window size if known. When nil, only the absolute token count is shown.
	WindowTokens *int
}
