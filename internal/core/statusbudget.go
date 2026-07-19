/**
 * Estimates the cell count available for context usage in a sidebar row.
 *
 * Herdr does not expose the runtime sidebar width via API, so we estimate
 * conservatively from the configured sidebar_width and the agent name.
 */
package core

// SidebarRowOverheadColumns approximates non-status overhead per row:
// leading indent, status dot, and padding around the name.
const SidebarRowOverheadColumns = 12

// EstimateStatusMaxColumns returns the budget for the context display token.
// sidebarWidth null/<=0 yields nil (full display).
func EstimateStatusMaxColumns(sidebarWidth *int, agentLabel *string) *int {
	if sidebarWidth == nil || *sidebarWidth <= 0 {
		return nil
	}
	name := ""
	if agentLabel != nil {
		name = *agentLabel
	}
	nameWidth := DisplayWidth(name)
	budget := *sidebarWidth - SidebarRowOverheadColumns - nameWidth
	// Floor so the shortest "N%" representation never gets clipped.
	if budget < 3 {
		budget = 3
	}
	return &budget
}
