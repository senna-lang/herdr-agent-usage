/**
 * Estimates the cell count available for context usage in a sidebar row.
 *
 * Herdr does not expose the runtime sidebar width via API, so callers resolve
 * the configured sidebar_width before passing it here.
 */
package core

// SidebarRowOverheadColumns approximates the non-status chrome herdr renders
// around the dedicated $context row: a 1-column vertical separator plus a
// 3-column row indent (herdrdev/herdr, src/ui/sidebar.rs, resolved_token_spans
// call sites). Herdr does not expose this value via its API, so it is not a
// guaranteed contract — a future herdr layout change could drift it out of
// sync with this constant.
const SidebarRowOverheadColumns = 4

// EstimateStatusMaxColumns returns the budget for the context display token.
// The token occupies its own row, so most of the sidebar width is available;
// SidebarRowOverheadColumns accounts for herdr's row separator and indent.
// sidebarWidth null/<=0 yields nil (full display).
func EstimateStatusMaxColumns(sidebarWidth *int) *int {
	if sidebarWidth == nil || *sidebarWidth <= 0 {
		return nil
	}
	budget := max(*sidebarWidth-SidebarRowOverheadColumns, 3)
	return &budget
}
