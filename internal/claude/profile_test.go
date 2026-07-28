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

func TestResolveActiveProfile_LoneSynthesizedDefaultAlwaysMatches(t *testing.T) {
	// Zero-config install: a relocated CLAUDE_CONFIG_DIR still attributes to the
	// synthesized ~/.claude profile, which is the only place both the write and
	// read sides agree on.
	profiles := ResolveProfiles(nil, map[string]string{"CLAUDE_CONFIG_DIR": "/x"}, "/home/u")
	p, ok := ResolveActiveProfile(profiles, "/totally/different", "/home/u")
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
	p, ok := ResolveActiveProfile(profiles, "/b", "/home/u")
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
	if _, ok := ResolveActiveProfile(profiles, "/unknown", "/home/u"); ok {
		t.Fatal("unknown CLAUDE_CONFIG_DIR must not match under multi-profile")
	}
}

func TestResolveActiveProfile_UnsetConfigDirMatchesDefaultDirProfile(t *testing.T) {
	// Bare `claude` sets no CLAUDE_CONFIG_DIR: the convention is to set it only
	// for additional accounts, so an empty value means the default ~/.claude
	// account and must match the profile declaring that dir.
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "base", ConfigDir: filepath.Join(home, ".claude")},
		{ID: "dev", ConfigDir: filepath.Join(home, ".claude-dev")},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	p, ok := ResolveActiveProfile(profiles, "", home)
	if !ok || p.ID != "base" {
		t.Fatalf("unset CLAUDE_CONFIG_DIR: ok=%v id=%q, want base", ok, p.ID)
	}
}

func TestResolveActiveProfile_UnsetConfigDirWithoutDefaultDirProfileSkips(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "dev", ConfigDir: filepath.Join(home, ".claude-dev")},
		{ID: "work", ConfigDir: filepath.Join(home, ".claude-work")},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	if _, ok := ResolveActiveProfile(profiles, "", home); ok {
		t.Fatal("default account must not be attributed to an unrelated profile")
	}
}

func TestResolveActiveProfile_TildeProfileMatchesAbsoluteConfigDir(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "base", ConfigDir: "~/.claude"},
		{ID: "dev", ConfigDir: "~/.claude-dev"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	p, ok := ResolveActiveProfile(profiles, filepath.Join(home, ".claude-dev"), home)
	if !ok || p.ID != "dev" {
		t.Fatalf("tilde config_dir must match absolute env: ok=%v id=%q", ok, p.ID)
	}
}

func TestResolveActiveProfile_TrailingSlashAndDotSegmentsMatch(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "base", ConfigDir: "/home/u/.claude/"},
		{ID: "dev", ConfigDir: "/home/u/./.claude-dev"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	if p, ok := ResolveActiveProfile(profiles, "/home/u/.claude", home); !ok || p.ID != "base" {
		t.Fatalf("trailing slash: ok=%v id=%q", ok, p.ID)
	}
	if p, ok := ResolveActiveProfile(profiles, "/home/u/.claude-dev/", home); !ok || p.ID != "dev" {
		t.Fatalf("dot segment: ok=%v id=%q", ok, p.ID)
	}
}

func TestResolveActiveProfile_SingleConfiguredProfileStillRequiresMatch(t *testing.T) {
	// One configured profile must not absorb every account: silently reporting
	// one account's usage as another's is worse than recording nothing.
	home := "/home/u"
	profiles := ResolveProfiles([]ProfileSpec{{ID: "dev", ConfigDir: "~/.claude-dev"}}, map[string]string{}, home)
	if _, ok := ResolveActiveProfile(profiles, filepath.Join(home, ".claude-other"), home); ok {
		t.Fatal("single configured profile must not match a foreign config dir")
	}
	if p, ok := ResolveActiveProfile(profiles, filepath.Join(home, ".claude-dev"), home); !ok || p.ID != "dev" {
		t.Fatalf("its own config dir must match: ok=%v id=%q", ok, p.ID)
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

func TestResolveProfiles_DefaultDirProfileUsesSiblingJSONPath(t *testing.T) {
	// Re-declaring the real default account (config_dir == ~/.claude) as an
	// explicit profile, alongside other accounts, must still resolve its
	// .claude.json to the sibling ~/.claude.json -- Claude Code never writes
	// that file inside ~/.claude/ itself.
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "claude", ConfigDir: filepath.Join(home, ".claude")},
		{ID: "claude-secondary", ConfigDir: "/other/dir"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	if profiles[0].JSONPath != filepath.Join(home, ".claude.json") {
		t.Fatalf("default-dir profile jsonPath = %q, want sibling ~/.claude.json", profiles[0].JSONPath)
	}
	// A genuinely separate config dir still defaults to <config_dir>/.claude.json.
	if profiles[1].JSONPath != filepath.Join("/other/dir", ".claude.json") {
		t.Fatalf("separate-dir profile jsonPath = %q", profiles[1].JSONPath)
	}
}

func TestResolveProfiles_ExplicitJSONPathOverridesDefaultDirHeuristic(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "claude", ConfigDir: filepath.Join(home, ".claude"), JSONPath: "/custom/path.json"},
	}
	p := ResolveProfiles(specs, map[string]string{}, home)[0]
	if p.JSONPath != "/custom/path.json" {
		t.Fatalf("explicit claude_json_path must win, got %q", p.JSONPath)
	}
}

func TestResolveProfiles_ExpandsTildeInPaths(t *testing.T) {
	// A "~" in config.toml is never expanded by a shell, so derived paths would
	// otherwise land under a directory literally named "~".
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "dev", ConfigDir: "~/.claude-dev", JSONPath: "~/custom/dev.json"},
	}
	p := ResolveProfiles(specs, map[string]string{}, home)[0]
	if p.ConfigDir != filepath.Join(home, ".claude-dev") {
		t.Fatalf("configDir = %q", p.ConfigDir)
	}
	if p.JSONPath != filepath.Join(home, "custom", "dev.json") {
		t.Fatalf("jsonPath = %q", p.JSONPath)
	}
	if p.LimitsCache != filepath.Join(home, ".claude-dev", "herdr-usagebar", "claude-limits-latest.json") {
		t.Fatalf("limitsCache = %q", p.LimitsCache)
	}
	if p.StateDir != filepath.Join(home, ".claude-dev", "herdr-usagebar") {
		t.Fatalf("stateDir = %q", p.StateDir)
	}
	if p.ProjectsRoot != filepath.Join(home, ".claude-dev", "projects") {
		t.Fatalf("projectsRoot = %q", p.ProjectsRoot)
	}
}

func TestResolveProfiles_TildeDefaultDirUsesSiblingJSONPath(t *testing.T) {
	// The sibling-.claude.json rule must survive tilde notation too.
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "base", ConfigDir: "~/.claude"},
		{ID: "dev", ConfigDir: "~/.claude-dev"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	if profiles[0].JSONPath != filepath.Join(home, ".claude.json") {
		t.Fatalf("tilde default-dir jsonPath = %q, want sibling ~/.claude.json", profiles[0].JSONPath)
	}
	if profiles[1].JSONPath != filepath.Join(home, ".claude-dev", ".claude.json") {
		t.Fatalf("tilde separate-dir jsonPath = %q", profiles[1].JSONPath)
	}
}

func TestResolveProfiles_DedupesTildeAndAbsoluteSameDir(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "base", ConfigDir: "~/.claude"},
		{ID: "base-again", ConfigDir: "/home/u/.claude/"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	if len(profiles) != 1 || profiles[0].ID != "base" {
		t.Fatalf("want only the first spec for one dir, got %+v", profiles)
	}
}

func TestResolveProfiles_RejectsRelativeConfigDir(t *testing.T) {
	// Resolving against the cwd would make the write side (cwd = project) and
	// the read side (cwd = Herdr) disagree, so a relative config_dir is
	// rejected outright (not kept and compared relatively). The only spec is
	// invalid, so this falls back to the non-implicit synthesized default.
	home := "/home/u"
	profiles := ResolveProfiles([]ProfileSpec{{ID: "rel", ConfigDir: "./.claude-rel"}}, map[string]string{}, home)
	if len(profiles) != 1 || profiles[0].ID != DefaultProfileID || profiles[0].Implicit {
		t.Fatalf("want non-implicit fallback default, got %+v", profiles[0])
	}
}

func TestResolveActiveProfile_RelativeConfigDirNeverMatches(t *testing.T) {
	// Even though the spec's (rejected) config_dir and the running
	// CLAUDE_CONFIG_DIR are textually identical relative strings, the spec
	// never became a profile and the fallback default must not match a
	// relative env value either.
	home := "/home/u"
	profiles := ResolveProfiles([]ProfileSpec{{ID: "rel", ConfigDir: "./.claude-rel"}}, map[string]string{}, home)
	if _, ok := ResolveActiveProfile(profiles, "./.claude-rel", home); ok {
		t.Fatal("relative CLAUDE_CONFIG_DIR must never match")
	}
}

func TestResolveActiveProfile_AllSpecsInvalidFallbackRequiresMatch(t *testing.T) {
	// Every configured entry is malformed (empty id here). ResolveProfiles
	// falls back to the default profile, but -- unlike true zero-config --
	// that fallback must not silently absorb an unrelated account's usage.
	home := "/home/u"
	specs := []ProfileSpec{{ID: "", ConfigDir: "~/.claude-dev"}}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	if len(profiles) != 1 || profiles[0].Implicit {
		t.Fatalf("want non-implicit fallback default, got %+v", profiles[0])
	}
	if _, ok := ResolveActiveProfile(profiles, filepath.Join(home, ".claude-dev"), home); ok {
		t.Fatal("malformed-config fallback must not absorb an unrelated account's CLAUDE_CONFIG_DIR")
	}
	// The default account itself must still work.
	if p, ok := ResolveActiveProfile(profiles, "", home); !ok || p.ID != DefaultProfileID {
		t.Fatalf("default account must still match: ok=%v id=%q", ok, p.ID)
	}
}

func TestValidProfileSpecCount(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "base", ConfigDir: "~/.claude"},
		{ID: "base-again", ConfigDir: "/home/u/.claude/"}, // duplicate dir
		{ID: "rel", ConfigDir: "./.claude-rel"},           // relative
		{ID: "", ConfigDir: "/home/u/.claude-noid"},       // missing id
	}
	if n := ValidProfileSpecCount(specs, home); n != 1 {
		t.Fatalf("ValidProfileSpecCount = %d, want 1", n)
	}
}
