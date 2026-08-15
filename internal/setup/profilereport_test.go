/**
 * Tests for the `usagebar setup` [[claude.profiles]] diagnostics.
 */
package setup

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
)

func reportText(specs []claude.ProfileSpec, home string) string {
	profiles := claude.ResolveProfiles(specs, map[string]string{}, home)
	return strings.Join(claudeProfileReportLines(specs, profiles, home), "\n")
}

func TestClaudeProfileReport_NoSpecsReportsSingleDefault(t *testing.T) {
	got := reportText(nil, "/home/u")
	if !strings.Contains(got, "none configured") ||
		!strings.Contains(got, filepath.Join("/home/u", ".claude")) {
		t.Fatalf("report = %q", got)
	}
	if strings.Contains(got, "!") {
		t.Fatalf("zero-config must not warn: %q", got)
	}
}

func TestClaudeProfileReport_AllEntriesIgnoredWarns(t *testing.T) {
	specs := []claude.ProfileSpec{{ID: "", ConfigDir: "/a"}}
	got := reportText(specs, "/home/u")
	if !strings.Contains(got, "all 1 [[claude.profiles]] entries were ignored") {
		t.Fatalf("report = %q", got)
	}
}

func TestClaudeProfileReport_MarksDefaultAccountAndListsProfiles(t *testing.T) {
	home := "/home/u"
	specs := []claude.ProfileSpec{
		{ID: "base", ConfigDir: "~/.claude"},
		{ID: "dev", ConfigDir: "~/.claude-dev"},
	}
	got := reportText(specs, home)
	if !strings.Contains(got, "2 configured") {
		t.Fatalf("count missing: %q", got)
	}
	if !strings.Contains(got, "base  "+filepath.Join(home, ".claude")+"  (default account)") {
		t.Fatalf("default marker missing: %q", got)
	}
	if !strings.Contains(got, "dev  "+filepath.Join(home, ".claude-dev")) {
		t.Fatalf("secondary row missing: %q", got)
	}
	if strings.Contains(got, "!") {
		t.Fatalf("valid config must not warn: %q", got)
	}
}

func TestClaudeProfileReport_WarnsWhenDefaultAccountUncovered(t *testing.T) {
	home := "/home/u"
	specs := []claude.ProfileSpec{
		{ID: "dev", ConfigDir: "~/.claude-dev"},
		{ID: "work", ConfigDir: "~/.claude-work"},
	}
	got := reportText(specs, home)
	if !strings.Contains(got, "no profile has config_dir = "+filepath.Join(home, ".claude")) {
		t.Fatalf("uncovered-default warning missing: %q", got)
	}
}

func TestClaudeProfileReport_WarnsOnDroppedDuplicateAndRelativeDir(t *testing.T) {
	// duplicate dir and a relative config_dir are both rejected before a
	// ClaudeProfile is ever built for them, so they surface only via the
	// dropped-entry count, not a per-profile row or a separate warning line.
	home := "/home/u"
	specs := []claude.ProfileSpec{
		{ID: "base", ConfigDir: "~/.claude"},
		{ID: "base-again", ConfigDir: "/home/u/.claude/"}, // duplicate dir -> dropped
		{ID: "rel", ConfigDir: "./.claude-rel"},           // relative -> dropped
	}
	got := reportText(specs, home)
	if !strings.Contains(got, "2 entries ignored") {
		t.Fatalf("dropped-entry warning missing: %q", got)
	}
	if strings.Contains(got, "rel") {
		t.Fatalf("rejected relative entry must not appear as a profile row: %q", got)
	}
}
