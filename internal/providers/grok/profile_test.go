/**
 * Tests Grok profile resolution keeps account stores distinct and fails closed.
 */
package grok

import (
	"path/filepath"
	"testing"
)

func TestResolveProfiles_DefaultWhenNoSpecs(t *testing.T) {
	home := "/home/u"
	profiles := ResolveProfiles(nil, map[string]string{}, home)

	if len(profiles) != 1 {
		t.Fatalf("profile count = %d, want 1", len(profiles))
	}
	if got := profiles[0]; got.ID != DefaultProfileID || got.Label != DefaultProfileLabel || got.Home != filepath.Join(home, ".grok") || !got.Implicit {
		t.Fatalf("default profile = %+v", got)
	}
}

func TestResolveProfiles_ConfiguredProfilesRemainDistinct(t *testing.T) {
	profiles := ResolveProfiles([]ProfileSpec{
		{ID: "personal", Label: "Personal", GrokHome: "/profiles/personal"},
		{ID: "work", GrokHome: "/profiles/work"},
	}, map[string]string{}, "/home/u")

	if len(profiles) != 2 {
		t.Fatalf("profile count = %d, want 2", len(profiles))
	}
	if got := profiles[0]; got.ID != "personal" || got.Label != "Personal" || got.Home != "/profiles/personal" || got.Implicit {
		t.Fatalf("first profile = %+v", got)
	}
	if got := profiles[1]; got.ID != "work" || got.Label != "work" || got.Home != "/profiles/work" || got.Implicit {
		t.Fatalf("second profile = %+v", got)
	}
}

func TestResolveProfiles_InvalidEntriesCannotCaptureAnotherHome(t *testing.T) {
	home := "/home/u"
	profiles := ResolveProfiles([]ProfileSpec{{ID: "work", GrokHome: "relative"}}, map[string]string{}, home)

	if len(profiles) != 1 || profiles[0].Implicit {
		t.Fatalf("profiles = %+v, want non-implicit fallback default", profiles)
	}
	if got := profiles[0].Home; got != filepath.Join(home, ".grok") {
		t.Fatalf("fallback home = %q", got)
	}
}

func TestResolveProfiles_RejectsDuplicateHomesAndIDs(t *testing.T) {
	profiles := ResolveProfiles([]ProfileSpec{
		{ID: "personal", GrokHome: "/profiles/personal"},
		{ID: "personal", GrokHome: "/profiles/work"},
		{ID: "work", GrokHome: "/profiles/personal"},
		{ID: "other", GrokHome: "/profiles/other"},
	}, map[string]string{}, "/home/u")

	if len(profiles) != 2 || profiles[0].ID != "personal" || profiles[1].ID != "other" {
		t.Fatalf("profiles = %+v", profiles)
	}
}
