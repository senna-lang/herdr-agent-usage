/**
 * Tests for the Codex multi-account profile model.
 */
package codex

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
	if p.ID != "codex" || p.Label != "Codex" {
		t.Fatalf("id/label = %q/%q", p.ID, p.Label)
	}
	if p.Home != filepath.Join(home, ".codex") {
		t.Fatalf("home = %q", p.Home)
	}
	if !p.Implicit {
		t.Fatal("zero-config default must be implicit")
	}
}

func TestResolveProfiles_DefaultIgnoresHomeEnv(t *testing.T) {
	// The synthesized default must stay anchored to ~/.codex regardless of
	// CODEX_HOME: that var is set on the Codex process (aliases) and is
	// invisible to the Herdr plugin action, so deriving the default off it
	// would make an unconfigured extra account overwrite the default row.
	home := "/home/u"
	p := ResolveProfiles(nil, map[string]string{"CODEX_HOME": "/alt/codex"}, home)[0]
	if p.Home != filepath.Join(home, ".codex") {
		t.Fatalf("home must ignore CODEX_HOME, got %q", p.Home)
	}
}

func TestResolveProfiles_MultipleProfiles(t *testing.T) {
	specs := []ProfileSpec{
		{ID: "codex", Label: "personal", CodexHome: "/a"},
		{ID: "dev", CodexHome: "/b"}, // label defaults to id
	}
	profiles := ResolveProfiles(specs, map[string]string{}, "/home/u")
	if len(profiles) != 2 {
		t.Fatalf("want 2, got %d", len(profiles))
	}
	if profiles[0].Label != "personal" || profiles[0].Home != "/a" {
		t.Fatalf("first = %+v", profiles[0])
	}
	if profiles[1].Label != "dev" || profiles[1].Home != "/b" {
		t.Fatalf("second = %+v", profiles[1])
	}
	if profiles[0].Implicit || profiles[1].Implicit {
		t.Fatal("configured profiles must not be implicit")
	}
}

func TestResolveProfiles_RejectsDuplicates(t *testing.T) {
	specs := []ProfileSpec{
		{ID: "codex", CodexHome: "/a"},
		{ID: "codex", CodexHome: "/c"}, // dup id
		{ID: "dev", CodexHome: "/a"},   // dup home
		{ID: "tester", CodexHome: "/d"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, "/home/u")
	if len(profiles) != 2 {
		t.Fatalf("want 2 after dedupe, got %d: %+v", len(profiles), profiles)
	}
	if profiles[0].ID != "codex" || profiles[1].ID != "tester" {
		t.Fatalf("unexpected survivors: %q, %q", profiles[0].ID, profiles[1].ID)
	}
}

func TestResolveProfiles_SkipsIncompleteEntries(t *testing.T) {
	specs := []ProfileSpec{
		{ID: "", CodexHome: "/a"},
		{ID: "dev", CodexHome: ""},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, "/home/u")
	if len(profiles) != 1 || profiles[0].ID != "codex" || profiles[0].Implicit {
		t.Fatalf("want non-implicit fallback default, got %+v", profiles)
	}
}

func TestIsCodexProviderID(t *testing.T) {
	profiles := []CodexProfile{{ID: "codex"}, {ID: "dev"}}
	if !IsCodexProviderID("dev", profiles) {
		t.Fatal("dev should be recognized")
	}
	if IsCodexProviderID("claude", profiles) {
		t.Fatal("claude should not be recognized")
	}
}

func TestResolveActiveProfile_LoneSynthesizedDefaultAlwaysMatches(t *testing.T) {
	profiles := ResolveProfiles(nil, map[string]string{"CODEX_HOME": "/x"}, "/home/u")
	p, ok := ResolveActiveProfile(profiles, "/totally/different", "/home/u")
	if !ok || p.ID != "codex" {
		t.Fatalf("single-profile fallback failed: ok=%v id=%q", ok, p.ID)
	}
}

func TestResolveActiveProfile_MultiMatchesHome(t *testing.T) {
	specs := []ProfileSpec{
		{ID: "codex", CodexHome: "/a"},
		{ID: "dev", CodexHome: "/b"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, "/home/u")
	p, ok := ResolveActiveProfile(profiles, "/b", "/home/u")
	if !ok || p.ID != "dev" {
		t.Fatalf("want dev, ok=%v id=%q", ok, p.ID)
	}
}

func TestResolveActiveProfile_MultiUnknownSkips(t *testing.T) {
	specs := []ProfileSpec{
		{ID: "codex", CodexHome: "/a"},
		{ID: "dev", CodexHome: "/b"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, "/home/u")
	if _, ok := ResolveActiveProfile(profiles, "/unknown", "/home/u"); ok {
		t.Fatal("unknown CODEX_HOME must not match under multi-profile")
	}
}

func TestResolveActiveProfile_UnsetHomeMatchesDefaultDirProfile(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "base", CodexHome: filepath.Join(home, ".codex")},
		{ID: "dev", CodexHome: filepath.Join(home, ".codex-dev")},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	p, ok := ResolveActiveProfile(profiles, "", home)
	if !ok || p.ID != "base" {
		t.Fatalf("unset CODEX_HOME: ok=%v id=%q, want base", ok, p.ID)
	}
}

func TestResolveActiveProfile_UnsetHomeWithoutDefaultDirProfileSkips(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "dev", CodexHome: filepath.Join(home, ".codex-dev")},
		{ID: "tester", CodexHome: filepath.Join(home, ".codex-tester")},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	if _, ok := ResolveActiveProfile(profiles, "", home); ok {
		t.Fatal("default account must not be attributed to an unrelated profile")
	}
}

func TestResolveActiveProfile_TildeProfileMatchesAbsoluteHome(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "base", CodexHome: "~/.codex"},
		{ID: "dev", CodexHome: "~/.codex-dev"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	p, ok := ResolveActiveProfile(profiles, filepath.Join(home, ".codex-dev"), home)
	if !ok || p.ID != "dev" {
		t.Fatalf("tilde codex_home must match absolute env: ok=%v id=%q", ok, p.ID)
	}
}

func TestResolveActiveProfile_SingleConfiguredProfileStillRequiresMatch(t *testing.T) {
	home := "/home/u"
	profiles := ResolveProfiles([]ProfileSpec{{ID: "dev", CodexHome: "~/.codex-dev"}}, map[string]string{}, home)
	if _, ok := ResolveActiveProfile(profiles, filepath.Join(home, ".codex-other"), home); ok {
		t.Fatal("single configured profile must not match a foreign home")
	}
	if p, ok := ResolveActiveProfile(profiles, filepath.Join(home, ".codex-dev"), home); !ok || p.ID != "dev" {
		t.Fatalf("its own home must match: ok=%v id=%q", ok, p.ID)
	}
}

func TestIsDefaultProfile(t *testing.T) {
	def := ResolveProfiles(nil, map[string]string{}, "/home/u")[0]
	if !IsDefaultProfile(def) {
		t.Fatal("synthesized default should be default")
	}
	custom := ResolveProfiles([]ProfileSpec{{ID: "dev", CodexHome: "/b"}}, map[string]string{}, "/home/u")[0]
	if IsDefaultProfile(custom) {
		t.Fatal("custom profile is not default")
	}
}

func TestResolveProfiles_ExpandsTildeInHome(t *testing.T) {
	home := "/home/u"
	p := ResolveProfiles([]ProfileSpec{{ID: "dev", CodexHome: "~/.codex-dev"}}, map[string]string{}, home)[0]
	if p.Home != filepath.Join(home, ".codex-dev") {
		t.Fatalf("home = %q", p.Home)
	}
}

func TestResolveProfiles_DedupesTildeAndAbsoluteSameHome(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "base", CodexHome: "~/.codex"},
		{ID: "base-again", CodexHome: "/home/u/.codex/"},
	}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	if len(profiles) != 1 || profiles[0].ID != "base" {
		t.Fatalf("want only the first spec for one home, got %+v", profiles)
	}
}

func TestResolveProfiles_RejectsRelativeHome(t *testing.T) {
	home := "/home/u"
	profiles := ResolveProfiles([]ProfileSpec{{ID: "rel", CodexHome: "./.codex-rel"}}, map[string]string{}, home)
	if len(profiles) != 1 || profiles[0].ID != DefaultProfileID || profiles[0].Implicit {
		t.Fatalf("want non-implicit fallback default, got %+v", profiles[0])
	}
}

func TestResolveActiveProfile_RelativeHomeNeverMatches(t *testing.T) {
	home := "/home/u"
	profiles := ResolveProfiles([]ProfileSpec{{ID: "rel", CodexHome: "./.codex-rel"}}, map[string]string{}, home)
	if _, ok := ResolveActiveProfile(profiles, "./.codex-rel", home); ok {
		t.Fatal("relative CODEX_HOME must never match")
	}
}

func TestResolveActiveProfile_AllSpecsInvalidFallbackRequiresMatch(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{{ID: "", CodexHome: "~/.codex-dev"}}
	profiles := ResolveProfiles(specs, map[string]string{}, home)
	if len(profiles) != 1 || profiles[0].Implicit {
		t.Fatalf("want non-implicit fallback default, got %+v", profiles[0])
	}
	if _, ok := ResolveActiveProfile(profiles, filepath.Join(home, ".codex-dev"), home); ok {
		t.Fatal("malformed-config fallback must not absorb an unrelated account's CODEX_HOME")
	}
	if p, ok := ResolveActiveProfile(profiles, "", home); !ok || p.ID != DefaultProfileID {
		t.Fatalf("default account must still match: ok=%v id=%q", ok, p.ID)
	}
}

func TestValidProfileSpecCount(t *testing.T) {
	home := "/home/u"
	specs := []ProfileSpec{
		{ID: "base", CodexHome: "~/.codex"},
		{ID: "base-again", CodexHome: "/home/u/.codex/"},
		{ID: "rel", CodexHome: "./.codex-rel"},
		{ID: "", CodexHome: "/home/u/.codex-noid"},
	}
	if n := ValidProfileSpecCount(specs, home); n != 1 {
		t.Fatalf("ValidProfileSpecCount = %d, want 1", n)
	}
}
