/**
 * Decides which collected providers are in use on this machine, so a host
 * can leave out harnesses the user has never installed or configured.
 */
package limits

import "os"

// MarkInUse sets InUse on every entry. An entry is in use when the user
// listed its family ([ui] providers, explicit), configured its profile, it has
// a reading, or its home directory exists and the adapter only lacks a login
// or a first reading (an installed but signed-out or idle harness). A source
// with nothing on disk (no install, no profile, no data) is not in use.
func MarkInUse(entries []ProviderLimits, explicit bool) []ProviderLimits {
	out := append([]ProviderLimits(nil), entries...)
	for i := range out {
		out[i].InUse = explicit || inUse(out[i])
	}
	return out
}

func inUse(p ProviderLimits) bool {
	if p.Configured || p.HasWindows() {
		return true
	}
	if p.Action == "" && !p.NoReadingYet {
		return false
	}
	return dirExists(p.Home)
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
