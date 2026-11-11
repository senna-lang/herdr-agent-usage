package limits

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
)

// Every provider that has a reading source on disk stamps where that source
// lives, so a host can tell an installed harness from one that is absent.
func TestProfileGroupingStampsIdentityHomeAndConfigured(t *testing.T) {
	grokProfile := grok.GrokProfile{ID: "grok-work", Label: "work", Home: "/homes/user-1/.grok-work"}
	openCodeProfile := opencode.OpenCodeProfile{ID: "opencode-work", Label: "work", DataDir: "/homes/user-1/oc-work"}

	for _, multi := range []bool{false, true} {
		got := applyGrokProfileGrouping(ProviderLimits{ProviderID: "grok-work"}, grokProfile, multi)
		if got.ProfileID != "grok-work" || got.ProfileLabel != "work" || got.Identity != grokProfile.Home || got.Home != grokProfile.Home || !got.Configured {
			t.Fatalf("grok (multi=%v) = %+v", multi, got)
		}
		if (got.GroupLabel == "Grok") != multi || (got.AccountLabel == "work") != multi {
			t.Fatalf("grok grouping (multi=%v) = %q/%q", multi, got.GroupLabel, got.AccountLabel)
		}

		got = applyOpenCodeProfileGrouping(ProviderLimits{ProviderID: "opencode-work"}, openCodeProfile, multi)
		if got.ProfileID != "opencode-work" || got.ProfileLabel != "work" || got.Identity != openCodeProfile.DataDir || got.Home != openCodeProfile.DataDir || !got.Configured {
			t.Fatalf("opencode (multi=%v) = %+v", multi, got)
		}
		if (got.GroupLabel == "OpenCode") != multi || (got.AccountLabel == "work") != multi {
			t.Fatalf("opencode grouping (multi=%v) = %q/%q", multi, got.GroupLabel, got.AccountLabel)
		}
	}

	implicitGrok := applyGrokProfileGrouping(ProviderLimits{}, grok.GrokProfile{ID: "grok", Label: "Grok", Home: "/homes/user-1/.grok", Implicit: true}, false)
	implicitOpenCode := applyOpenCodeProfileGrouping(ProviderLimits{}, opencode.OpenCodeProfile{ID: "opencode", Label: "OpenCode", DataDir: "/homes/user-1/oc", Implicit: true}, false)
	if implicitGrok.Configured || implicitOpenCode.Configured {
		t.Fatal("an implicit default profile must not count as configured")
	}
}

// A harness that is installed but has never produced a reading is "no data",
// the state hosts use to keep it listed rather than hide it.
func TestNoReadingYetIsSetForCodexAndAntigravity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USAGEBAR_STATE_DIR", t.TempDir())

	codexHome := t.TempDir()
	codexLimits := CollectCodexLimitsIn(codexHome, "codex", "Codex", 1_800_000_000_000)
	if !codexLimits.NoReadingYet || codexLimits.Status() != StatusNoData {
		t.Fatalf("codex without rollouts = %+v (status %q)", codexLimits, codexLimits.Status())
	}

	agy := CollectAntigravityLimits(1_800_000_000_000, CollectAntigravityLimitsOptions{SessionsDir: t.TempDir()})
	if !agy.NoReadingYet || agy.Status() != StatusNoData {
		t.Fatalf("antigravity without a snapshot = %+v (status %q)", agy, agy.Status())
	}
}

// The single-account collectors report their home dir.
func TestDefaultCollectOptionsStampSingleCollectorHomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", t.TempDir())
	stateDir := filepath.Join(home, "state")
	kiloDir := filepath.Join(home, "kilo-data")
	for _, dir := range []string{stateDir, kiloDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("USAGEBAR_STATE_DIR", stateDir)
	t.Setenv("KILO_DATA_DIR", kiloDir)

	opts := DefaultCollectOptions()
	const nowMs = 1_800_000_000_000

	if got := opts.Antigravity(nil, nowMs); got.Home != stateDir {
		t.Fatalf("antigravity Home = %q, want %q", got.Home, stateDir)
	}
	if got := opts.Kilo(nil, nowMs); got.Home != kiloDir {
		t.Fatalf("kilo Home = %q, want %q", got.Home, kiloDir)
	}
}
