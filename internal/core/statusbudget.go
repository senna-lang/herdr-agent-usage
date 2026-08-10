/**
 * Estimates the cell count available for context usage in a sidebar row.
 *
 * Herdr does not expose the runtime sidebar width via API, so callers resolve
 * the configured sidebar_width before passing it here.
 */
package core

// EstimateStatusMaxColumns returns the budget for the context display token.
// The token occupies its own row, so the full sidebar width is available.
// sidebarWidth null/<=0 yields nil (full display).
func EstimateStatusMaxColumns(sidebarWidth *int) *int {
	if sidebarWidth == nil || *sidebarWidth <= 0 {
		return nil
	}
	budget := *sidebarWidth
	return &budget
}
