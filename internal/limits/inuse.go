/**
 * Decides which collected providers are in use on this machine, so a host
 * can leave out harnesses the user has never installed or configured.
 */
package limits

import "os"

// MarkInUse sets InUse on every entry. An entry is in use when the user
// listed its family ([ui] providers, explicit), configured its profile, it has
// a reading, or the adapter stamped Installed. A signed-out harness with a
// login action and an existing home is also in use. Adapters decide what
// counts as installed; this check does not branch on provider name. A source
// with nothing on disk is not in use.
func MarkInUse(entries []ProviderLimits, explicit bool) []ProviderLimits {
	out := append([]ProviderLimits(nil), entries...)
	for i := range out {
		out[i].InUse = explicit || inUse(out[i])
	}
	return out
}

func inUse(p ProviderLimits) bool {
	if p.Configured || p.HasWindows() || p.Installed {
		return true
	}
	if p.Action != "" && dirExists(p.Home) {
		return true
	}
	return false
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
