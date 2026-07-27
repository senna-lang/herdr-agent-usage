/**
 * Resolves the sidebar's row-1 identity: a stand-in for Herdr's built-in
 * `tab`/`pane` row tokens, which go blank whenever a tab keeps its
 * auto-assigned numeric label (e.g. a fresh tab defaults to "1") and the
 * pane itself was never renamed.
 */
package core

import "strconv"

// TabLabelIsDefault reports whether label is the auto-assigned label Herdr
// gives an un-renamed tab: its own number as a string (a fresh tab 1 reports
// label "1", not ""). Exported so a caller can skip fetching the workspace
// name — the fallback source — when the tab already names itself.
func TabLabelIsDefault(label string, number int) bool {
	return label == "" || label == strconv.Itoa(number)
}

// ResolveSidebarTitle composes the sidebar's row-1 text.
//
//   - tabLabel is treated as unset per TabLabelIsDefault.
//   - When tabLabel is unset, workspaceLabel (the "space" name) fills in.
//   - paneLabel (a pane rename) is appended after "・" when set; with no
//     base label it stands alone rather than leaving a stray separator.
func ResolveSidebarTitle(paneLabel, tabLabel string, tabNumber int, workspaceLabel string) string {
	base := tabLabel
	if TabLabelIsDefault(base, tabNumber) {
		base = workspaceLabel
	}
	switch {
	case paneLabel == "":
		return base
	case base == "":
		return paneLabel
	default:
		return base + "・" + paneLabel
	}
}
