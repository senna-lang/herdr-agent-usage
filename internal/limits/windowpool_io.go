/**
 * Disk side of the window pool: gathers every local observation.
 */
package limits

import "github.com/senna-lang/herdr-agent-usage/internal/providers/omp"

// ObserveAccountWindows returns every rate-limit observation available on
// this machine.
//
// OMP's usage_history is currently the only observer that records windows for
// accounts beyond its own vendor CLI's, so it is the only source here; the
// vendor CLIs stay each collector's first-hand tier. Adding an observer means
// appending to this function, not touching the collectors.
func ObserveAccountWindows() []AccountWindows {
	return AccountWindowsFromOMP(omp.LatestUsageWindows(omp.ResolveAgentDBPath()))
}

// borrowWindows builds providerID's row from another agent's observation of
// account wantAccount, or returns nil when nothing may be borrowed. Pass an
// empty wantAccount when the collector cannot name its account.
func borrowWindows(providerID, label, wantAccount string, nowMs int64) *ProviderLimits {
	selected := SelectAccountWindows(ObserveAccountWindows(), providerID, wantAccount)
	if selected == nil {
		return nil
	}
	borrowed := BorrowedProviderLimits(*selected, providerID, label, nowMs)
	return &borrowed
}
