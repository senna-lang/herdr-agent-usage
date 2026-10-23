/**
 * Colour bands for the provider limit row.
 *
 * Sidebar tokens are plain text: Herdr colours $limit_high / $limit_mid /
 * $limit_low from config.toml, not from ANSI inside the value. Exactly one band
 * carries the text at a time, the same shape the cache row uses.
 *
 * Bands track the *tightest* window on the pane, so the colour answers "how
 * close am I to a wall", not "how is the current window doing".
 */
package core

import "strings"

// LimitBand is which styled sidebar token should carry the limit row.
type LimitBand string

const (
	// LimitHigh is the unstyled default: comfortable headroom.
	LimitHigh LimitBand = "limit_high"
	LimitMid  LimitBand = "limit_mid"
	LimitLow  LimitBand = "limit_low"
)

// LimitTokenNames is the exclusive set written for one pane's limit row.
var LimitTokenNames = []string{string(LimitHigh), string(LimitMid), string(LimitLow)}

// Limit band thresholds, in percent remaining.
const (
	LimitHighFloorPercent = 50
	LimitMidFloorPercent  = 25
)

// LimitBandFor maps remaining headroom onto the styled token:
// ≥50% high/default, ≥25% mid/yellow, otherwise low/red.
func LimitBandFor(remainingPercent float64) LimitBand {
	if remainingPercent >= LimitHighFloorPercent {
		return LimitHigh
	}
	if remainingPercent >= LimitMidFloorPercent {
		return LimitMid
	}
	return LimitLow
}

// Suffix is the trailing token-name segment ("high", "mid", "low"), so a
// per-window token can be assembled as limit_<tag>_<suffix>.
func (b LimitBand) Suffix() string {
	return strings.TrimPrefix(string(b), "limit_")
}
