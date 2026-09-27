/**
 * Tests for the `usagebar setup` [[claude.profiles]] diagnostics.
 */
package setup

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/senna-lang/herdr-agent-usage/internal/providers/claude"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/codex"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/grok"
	"github.com/senna-lang/herdr-agent-usage/internal/providers/opencode"
	"github.com/senna-lang/herdr-agent-usage/internal/testpath"
)

func reportText(specs []claude.ProfileSpec, home string) string {
	profiles := claude.ResolveProfiles(specs, map[string]string{}, home)
	return strings.Join(claudeProfileReportLines(specs, profiles, home), "\n")
}

func TestClaudeProfileReport_NoSpecsReportsSingleDefault(t *testing.T) {
	got := reportText(nil, testpath.Abs("/home/u"))
	if !strings.Contains(got, "none configured") ||
		!strings.Contains(got, filepath.Join(testpath.Abs("/home/u"), ".claude")) {
		t.Fatalf("report = %q", got)
	}
	if strings.Contains(got, "!") {
		t.Fatalf("zero-config must not warn: %q", got)
	}
}

func TestClaudeProfileReport_AllEntriesIgnoredWarns(t *testing.T) {
	specs := []claude.ProfileSpec{{ID: "", ConfigDir: testpath.Abs("/a")}}
	got := reportText(specs, testpath.Abs("/home/u"))
	if !strings.Contains(got, "all 1 [[claude.profiles]] entries were ignored") {
		t.Fatalf("report = %q", got)
	}
}

func TestClaudeProfileReport_MarksDefaultAccountAndListsProfiles(t *testing.T) {
	home := testpath.Abs("/home/u")
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
	home := testpath.Abs("/home/u")
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
	home := testpath.Abs("/home/u")
	specs := []claude.ProfileSpec{
		{ID: "base", ConfigDir: "~/.claude"},
		{ID: "base-again", ConfigDir: testpath.Abs("/home/u/.claude/")}, // duplicate dir -> dropped
		{ID: "rel", ConfigDir: "./.claude-rel"},                         // relative -> dropped
	}
	got := reportText(specs, home)
	if !strings.Contains(got, "2 entries ignored") {
		t.Fatalf("dropped-entry warning missing: %q", got)
	}
	if strings.Contains(got, "rel") {
		t.Fatalf("rejected relative entry must not appear as a profile row: %q", got)
	}
}

func codexReportText(specs []codex.ProfileSpec, home string) string {
	profiles := codex.ResolveProfiles(specs, map[string]string{}, home)
	return strings.Join(codexProfileReportLines(specs, profiles, home), "\n")
}

func TestCodexProfileReport_NoSpecsReportsSingleDefault(t *testing.T) {
	got := codexReportText(nil, testpath.Abs("/home/u"))
	if !strings.Contains(got, "none configured") ||
		!strings.Contains(got, filepath.Join(testpath.Abs("/home/u"), ".codex")) {
		t.Fatalf("report = %q", got)
	}
	if strings.Contains(got, "!") {
		t.Fatalf("zero-config must not warn: %q", got)
	}
}

func TestCodexProfileReport_AllEntriesIgnoredWarns(t *testing.T) {
	specs := []codex.ProfileSpec{{ID: "", CodexHome: testpath.Abs("/a")}}
	got := codexReportText(specs, testpath.Abs("/home/u"))
	if !strings.Contains(got, "all 1 [[codex.profiles]] entries were ignored") {
		t.Fatalf("report = %q", got)
	}
}

func TestCodexProfileReport_MarksDefaultAccountAndListsProfiles(t *testing.T) {
	home := testpath.Abs("/home/u")
	specs := []codex.ProfileSpec{
		{ID: "base", CodexHome: "~/.codex"},
		{ID: "dev", CodexHome: "~/.codex-dev"},
	}
	got := codexReportText(specs, home)
	if !strings.Contains(got, "2 configured") {
		t.Fatalf("count missing: %q", got)
	}
	if !strings.Contains(got, "base  "+filepath.Join(home, ".codex")+"  (default account)") {
		t.Fatalf("default marker missing: %q", got)
	}
	if !strings.Contains(got, "dev  "+filepath.Join(home, ".codex-dev")) {
		t.Fatalf("secondary row missing: %q", got)
	}
	if strings.Contains(got, "!") {
		t.Fatalf("valid config must not warn: %q", got)
	}
}

func TestCodexProfileReport_WarnsWhenDefaultAccountUncovered(t *testing.T) {
	home := testpath.Abs("/home/u")
	specs := []codex.ProfileSpec{
		{ID: "dev", CodexHome: "~/.codex-dev"},
		{ID: "tester", CodexHome: "~/.codex-tester"},
	}
	got := codexReportText(specs, home)
	if !strings.Contains(got, "no profile has codex_home = "+filepath.Join(home, ".codex")) {
		t.Fatalf("uncovered-default warning missing: %q", got)
	}
}

func TestGrokProfileReport_ListsProfilesAndWarnsWhenDefaultUncovered(t *testing.T) {
	home := testpath.Abs("/home/u")
	specs := []grok.ProfileSpec{{ID: "work", GrokHome: testpath.Abs("/accounts/work")}}
	profiles := grok.ResolveProfiles(specs, map[string]string{}, home)
	report := strings.Join(grokProfileReportLines(specs, profiles, home), "\n")

	if !strings.Contains(report, "work  "+testpath.Abs("/accounts/work")) {
		t.Fatalf("report = %q", report)
	}
	if !strings.Contains(report, "no profile has grok_home") {
		t.Fatalf("missing uncovered-default warning: %q", report)
	}
}

func TestOpenCodeProfileReport_ListsProfilesAndWarnsWhenDefaultUncovered(t *testing.T) {
	home := testpath.Abs("/home/u")
	specs := []opencode.ProfileSpec{{ID: "work", DataDir: testpath.Abs("/accounts/work")}}
	profiles := opencode.ResolveProfiles(specs, map[string]string{}, home)
	report := strings.Join(openCodeProfileReportLines(specs, profiles, map[string]string{}, home), "\n")

	if !strings.Contains(report, "work  "+testpath.Abs("/accounts/work")) {
		t.Fatalf("report = %q", report)
	}
	if !strings.Contains(report, "no profile has data_dir") {
		t.Fatalf("missing uncovered-default warning: %q", report)
	}
}
