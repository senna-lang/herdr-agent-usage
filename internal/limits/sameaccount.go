/**
 * Joins entries that are the same account seen through two config dirs.
 *
 * A config dir whose login is switched (e.g. the everyday ~/.claude, signed
 * into whichever account is in use) holds the same account as a dedicated
 * profile. Its sessions keep that account's windows fresh while the
 * dedicated profile's own files age. Since a rate-limit window belongs to the
 * account (see limitscore/windowpool.go), every entry of that account shows
 * the freshest reading, and later entries point at the first so renderers
 * can show the account once.
 */
package limits

// ShareAccountWindows applies the join to entries in collection order. Only
// entries of one family with the same non-empty Account are joined; an
// entry whose account cannot be read is never joined.
func ShareAccountWindows(entries []ProviderLimits) []ProviderLimits {
	type key struct{ family, account string }
	groups := map[key][]int{}
	var order []key
	for i, e := range entries {
		if e.Account == "" {
			continue
		}
		k := key{e.Family, e.Account}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}
	out := append([]ProviderLimits(nil), entries...)
	for _, k := range order {
		members := groups[k]
		if len(members) < 2 {
			continue
		}
		freshest := freshestWithWindows(out, members)
		first := out[members[0]].ProviderID
		for n, i := range members {
			if freshest >= 0 && i != freshest && out[freshest].FetchedAtMs > out[i].FetchedAtMs {
				out[i] = withWindowsOf(out[i], out[freshest])
			}
			if n > 0 {
				out[i].SameAccountAs = first
			}
		}
	}
	return out
}

// freshestWithWindows is the index of the newest member reading that has any
// window, or -1 when none has.
func freshestWithWindows(entries []ProviderLimits, members []int) int {
	best := -1
	for _, i := range members {
		if !entries[i].HasWindows() {
			continue
		}
		if best < 0 || entries[i].FetchedAtMs > entries[best].FetchedAtMs {
			best = i
		}
	}
	return best
}

// withWindowsOf gives dst src's observation: its windows, age, staleness and
// note, attributed to the profile it came from. dst keeps its own identity.
func withWindowsOf(dst, src ProviderLimits) ProviderLimits {
	dst.Primary, dst.Secondary, dst.Tertiary = src.Primary, src.Secondary, src.Tertiary
	dst.FetchedAtMs = src.FetchedAtMs
	dst.StaleMinutes = src.StaleMinutes
	dst.Note = src.Note
	dst.Action = src.Action
	dst.Source = src.Source
	if src.ProfileLabel != "" {
		dst.Source = src.Source + " via " + src.ProfileLabel
	}
	if dst.PlanType == nil {
		dst.PlanType = src.PlanType
	}
	return dst
}
