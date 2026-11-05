/**
 * Regression coverage for the $limit colour-band token lifecycle: band
 * transitions on working panes must not double-render, non-percentage rows
 * must stay visible in the default-coloured band, and the retain policy must
 * protect last-known-good values only while a row is empty.
 */
package update

import (
	"strings"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

// bandRecorder captures one pass of writes: what got set (with value) and what
// got cleared.
type bandRecorder struct {
	set     map[string]string
	cleared map[string]bool
}

func newBandRecorder() *bandRecorder {
	return &bandRecorder{set: map[string]string{}, cleared: map[string]bool{}}
}

func (r *bandRecorder) writer() metadataTokenWriter {
	return metadataTokenWriter{
		set: func(paneID, _, name, value string) bool {
			r.set[paneID+"/"+name] = value
			return true
		},
		clear: func(paneID, _, name string) bool {
			r.cleared[paneID+"/"+name] = true
			return true
		},
	}
}

func TestWriteLimitBandTokens_WorkingPaneBandTransitionClearsOldBand(t *testing.T) {
	rec := newBandRecorder()
	// Pane is working (retain=true) and was previously in the high band.
	current := map[string]string{"limit_high": "5h 94%"}
	writeLimitBandTokensWith(rec.writer(), current, "w1:p1", "5h 38%", 38, false, true)

	if rec.set["w1:p1/limit_mid"] != "5h 38%" {
		t.Fatalf("mid band not set: %v", rec.set)
	}
	if !rec.cleared["w1:p1/limit_high"] {
		t.Fatalf("stale high band survived the transition: set=%v cleared=%v", rec.set, rec.cleared)
	}
}

func TestWriteLimitBandTokens_BurnTextKeptInDefaultBand(t *testing.T) {
	rec := newBandRecorder()
	// Pay-as-you-go burn: non-empty text, no percentage (remaining -1).
	current := map[string]string{"limit_mid": "stale 5h 60%"}
	writeLimitBandTokensWith(rec.writer(), current, "w1:p1", "Σ 475k", -1, false, true)

	if rec.set["w1:p1/limit_high"] != "Σ 475k" {
		t.Fatalf("burn text not published in the default-colour band: %v", rec.set)
	}
	if !rec.cleared["w1:p1/limit_mid"] {
		t.Fatalf("stale mid band not cleared: %v", rec.cleared)
	}
}

func TestWriteLimitBandTokens_WorkingPaneEmptyRowRetainsEverything(t *testing.T) {
	rec := newBandRecorder()
	current := map[string]string{"limit_high": "5h 94%"}
	writeLimitBandTokensWith(rec.writer(), current, "w1:p1", "", -1, false, true)

	if len(rec.set) != 0 || len(rec.cleared) != 0 {
		t.Fatalf("empty row on a working pane must not touch any band: %v %v", rec.set, rec.cleared)
	}
}

func TestWriteLimitBandTokens_SettledEmptyRowClearsFamily(t *testing.T) {
	rec := newBandRecorder()
	current := map[string]string{
		string(core.LimitHigh): "5h 94%",
		string(core.LimitMid):  "5h 40%",
		string(core.LimitLow):  "5h 10%",
	}
	writeLimitBandTokensWith(rec.writer(), current, "w1:p1", "", -1, false, false)

	for _, name := range core.LimitTokenNames {
		if !rec.cleared["w1:p1/"+name] {
			t.Fatalf("%s not cleared: %v", name, rec.cleared)
		}
	}
}

func TestWriteWindowBandTokens_WorkingPaneNoWindowsRetains(t *testing.T) {
	rec := newBandRecorder()
	current := map[string]string{"limit_5h_high": "94% 3h"}
	writeWindowBandTokensWith(rec.writer(), current, "w1:p1", nil, true)

	if len(rec.set) != 0 || len(rec.cleared) != 0 {
		t.Fatalf("working pane must retain window tokens: %v %v", rec.set, rec.cleared)
	}
}

func TestWriteWindowBandTokens_SlotSwapClearsOldSuffix(t *testing.T) {
	rec := newBandRecorder()
	// Previously the 5h window sat in "high"; it dropped to "mid".
	current := map[string]string{
		"limit_5h_high": "94% 3h",
		"limit_7d_high": "96% 3d",
	}
	bands := []limits.SidebarWindowBand{
		{Tag: "5h", Text: "38% 3h", Band: core.LimitMid},
		{Tag: "7d", Text: "96% 3d", Band: core.LimitHigh},
	}
	writeWindowBandTokensWith(rec.writer(), current, "w1:p1", bands, false)

	if rec.set["w1:p1/limit_5h_mid"] != "38% 3h" {
		t.Fatalf("5h mid not written: %v", rec.set)
	}
	if !rec.cleared["w1:p1/limit_5h_high"] {
		t.Fatalf("stale 5h high not cleared: %v", rec.cleared)
	}
	if rec.set["w1:p1/limit_7d_high"] != "" {
		t.Fatalf("unchanged 7d high rewritten: %v", rec.set)
	}
	// No 30d window is present, so no 30d slot may carry a value.
	for name, value := range rec.set {
		if strings.Contains(name, "limit_30d") && value != "" {
			t.Fatalf("absent 30d window got a value: %v", rec.set)
		}
	}
}
