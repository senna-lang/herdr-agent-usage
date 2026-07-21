/**
 * Tests for the Claude multi-account profile model.
 */
package claude

import (
	"path/filepath"
	"testing"
)

func TestResolveProfiles_DefaultWhenNoSpecs(t *testing.T) {
	home := "/home/u"
	profiles := ResolveProfiles(nil, map[string]string{}, home)
	if len(profiles) != 1 {
		t.Fatalf("want 1 default profile, got %d", len(profiles))
	}
	p := profiles[0]
	if p.ID != "claude" || p.Label != "Claude" {
		t.Fatalf("id/label = %q/%q", p.ID, p.Label)
	}
	if p.ConfigDir != filepath.Join(home, ".claude") {
		t.Fatalf("configDir = %q", p.ConfigDir)
	}
	// Byte-identical to historical defaults.
	if p.LimitsCache != filepath.Join(home, ".claude", "herdr-usagebar", "claude-limits-latest.json") {
		t.Fatalf("limitsCache = %q", p.LimitsCache)
	}
	if p.StateDir != filepath.Join(home, ".claude", "herdr-usagebar") {
		t.Fatalf("stateDir = %q", p.StateDir)
	}
	if p.ProjectsRoot != filepath.Join(home, ".claude", "projects") {
		t.Fatalf("projectsRoot = %q", p.ProjectsRoot)
	}
	if p.JSONPath != filepath.Join(home, ".claude.json") {
		t.Fatalf("jsonPath = %q", p.JSONPath)
	}
}

func TestResolveProfiles_DefaultHonorsEnvOverrides(t *testing.T) {
	home := "/home/u"
	env := map[string]string{
		"USAGEBAR_CLAUDE_LIMITS_PATH": "/override/limits.json",
		"USAGEBAR_STATE_DIR":          "/override/state",
		"CLAUDE_PROJECTS_ROOT":        "/override/projects",
		"CLAUDE_CONFIG_JSON":          "/override/.claude.json",
	}
	p := ResolveProfiles(nil, env, home)[0]
	if p.LimitsCache != "/override/limits.json" {
		t.Fatalf("limitsCache = %q", p.LimitsCache)
	}
	if p.StateDir != "/override/state" {
		t.Fatalf("stateDir = %q", p.StateDir)
	}
	if p.ProjectsRoot != "/override/projects" {
		t.Fatalf("projectsRoot = %q", p.ProjectsRoot)
	}
	if p.JSONPath != "/override/.claude.json" {
		t.Fatalf("jsonPath = %q", p.JSONPath)
	}
}

func TestResolveProfiles_DefaultIgnoresConfigDirEnv(t *testing.T) {
	// The synthesized default must stay anchored to ~/.claude regardless of
	// CLAUDE_CONFIG_DIR: that var is visible to the write side (statusLine,
	// in-process) but invisible to the read side (panel/sidebar, a Herdr plugin
	// action), so deriving the default off it would make the two sides read and
	// write different files for the same unconfigured account.
	home := "/home/u"
	p := ResolveProfiles(nil, map[string]string{"CLAUDE_CONFIG_DIR": "/alt/cfg"}, home)[0]
	if p.ConfigDir != filepath.Join(home, ".claude") {
		t.Fatalf("configDir must ignore CLAUDE_CONFIG_DIR, got %q", p.ConfigDir)
	}
	if p.LimitsCache != filepath.Join(home, ".claude", "herdr-usagebar", "claude-limits-latest.json") {
		t.Fatalf("limitsCache = %q", p.LimitsCache)
	}
	if p.ProjectsRoot != filepath.Join(home, ".claude", "projects") {
		t.Fatalf("projectsRoot = %q", p.ProjectsRoot)
	}
}

func TestResolveProfiles_MultipleProfiles(t *testing.T) {
	specs := []ProfileSpec{
		{ID: "claude", Label: "Claude", ConfigDir: "/a"},
		{ID: "claude-m", ConfigDir: "/b"}, // label defaults to id
	}
	profiles := ResolveProfiles(specs, map[string]string{}, "/home/u")
	if len(profiles) != 2 {
		t.Fatalf("want 2, got %d", len(profiles))
	}
	if profiles[1].Label != "claude-m" {
		t.Fatalf("label default = %q", profiles[1].Label)
	}
	if profiles[1].JSONPath != filepath.Join("/b", ".claude.json") {
		t.Fatalf("jsonPath default = %q", profiles[1].JSONPath)
	}
	if profiles[0].LimitsCache != filepath.Join("/a", "herdr-usagebar", "claude-limits-latest.json") {
		t.Fatalf("limitsCache = %q", profiles[0].LimitsCache)
	}
}

func TestResolveProfiles_MultiIgnoresEnvOverrides(t *testing.T) {
	// A global override cannot be attributed to one of several profiles.
	specs := []ProfileSpec{
		{ID: "claude", ConfigDir: "/a"},
		{ID: "claude-m", ConfigDir: "/b"},
	}
	env := map[string]string{"USAGEBAR_CLAUDE_LIMITS_PATH": "/override/limits.json"}
	profiles := ResolveProfiles(specs, env, "/home/u")
	for _, p := range profiles {
		if p.LimitsCache == "/override/limits.json" {
			t.Fatalf("multi mode must ignore global override, got %q", p.LimitsCache)
		}
	}
}

func TestResolveProfiles_RejectsDuplicates(t *testing.T) {
	specs := []ProfileSpec{
		{ID: "claude", ConfigDir: "/a"},
		{ID: "claude", ConfigDir: "/c"},   // dup id
		{ID: "claude-x", ConfigDir: "/a"}, // dup config dir
		{ID: "claude-y", ConfigDir: "/d"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, "/home/u")
	if len(profiles) != 2 {
		t.Fatalf("want 2 after dedupe, got %d: %+v", len(profiles), profiles)
	}
	if profiles[0].ID != "claude" || profiles[1].ID != "claude-y" {
		t.Fatalf("unexpected survivors: %q, %q", profiles[0].ID, profiles[1].ID)
	}
}

func TestResolveProfiles_SkipsIncompleteEntries(t *testing.T) {
	specs := []ProfileSpec{
		{ID: "", ConfigDir: "/a"},       // missing id
		{ID: "claude-z", ConfigDir: ""}, // missing config dir
	}
	// All invalid -> falls back to synthesized default.
	profiles := ResolveProfiles(specs, map[string]string{}, "/home/u")
	if len(profiles) != 1 || profiles[0].ID != "claude" {
		t.Fatalf("want fallback default, got %+v", profiles)
	}
}

func TestIsClaudeProviderID(t *testing.T) {
	profiles := []ClaudeProfile{{ID: "claude"}, {ID: "claude-secondary"}}
	if !IsClaudeProviderID("claude-secondary", profiles) {
		t.Fatal("claude-secondary should be recognized")
	}
	if IsClaudeProviderID("codex", profiles) {
		t.Fatal("codex should not be recognized")
	}
}

func TestResolveActiveProfile_SingleAlwaysMatches(t *testing.T) {
	profiles := ResolveProfiles(nil, map[string]string{"CLAUDE_CONFIG_DIR": "/x"}, "/home/u")
	p, ok := ResolveActiveProfile(profiles, "/totally/different")
	if !ok || p.ID != "claude" {
		t.Fatalf("single-profile fallback failed: ok=%v id=%q", ok, p.ID)
	}
}

func TestResolveActiveProfile_MultiMatchesConfigDir(t *testing.T) {
	specs := []ProfileSpec{
		{ID: "claude", ConfigDir: "/a"},
		{ID: "claude-m", ConfigDir: "/b"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, "/home/u")
	p, ok := ResolveActiveProfile(profiles, "/b")
	if !ok || p.ID != "claude-m" {
		t.Fatalf("want claude-m, ok=%v id=%q", ok, p.ID)
	}
}

func TestResolveActiveProfile_MultiUnknownSkips(t *testing.T) {
	specs := []ProfileSpec{
		{ID: "claude", ConfigDir: "/a"},
		{ID: "claude-m", ConfigDir: "/b"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, "/home/u")
	if _, ok := ResolveActiveProfile(profiles, "/unknown"); ok {
		t.Fatal("unknown CLAUDE_CONFIG_DIR must not match under multi-profile")
	}
}

func TestIsDefaultProfile(t *testing.T) {
	def := ResolveProfiles(nil, map[string]string{}, "/home/u")[0]
	if !IsDefaultProfile(def) {
		t.Fatal("synthesized default should be default")
	}
	custom := ResolveProfiles([]ProfileSpec{{ID: "claude-m", ConfigDir: "/b"}}, map[string]string{}, "/home/u")[0]
	if IsDefaultProfile(custom) {
		t.Fatal("custom profile is not default")
	}
}
