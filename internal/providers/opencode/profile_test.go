/**
 * Tests OpenCode profile resolution keeps account data directories distinct.
 */
package opencode

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
	if got := profiles[0]; got.ID != DefaultProfileID || got.Label != DefaultProfileLabel || got.DataDir != filepath.Join(home, ".local", "share", "opencode") || !got.Implicit {
		t.Fatalf("default profile = %+v", got)
	}
}

func TestResolveProfiles_UsesXDGDefaultWhenConfigured(t *testing.T) {
	profiles := ResolveProfiles(nil, map[string]string{"XDG_DATA_HOME": "/data"}, "/home/u")

	if got := profiles[0].DataDir; got != "/data/opencode" {
		t.Fatalf("data dir = %q", got)
	}
}

func TestResolveProfiles_ConfiguredProfilesRemainDistinct(t *testing.T) {
	profiles := ResolveProfiles([]ProfileSpec{
		{ID: "personal", Label: "Personal", DataDir: "/data/personal"},
		{ID: "work", DataDir: "/data/work"},
	}, map[string]string{}, "/home/u")

	if len(profiles) != 2 {
		t.Fatalf("profile count = %d, want 2", len(profiles))
	}
	if got := profiles[0]; got.ID != "personal" || got.Label != "Personal" || got.DataDir != "/data/personal" || got.Implicit {
		t.Fatalf("first profile = %+v", got)
	}
	if got := profiles[1]; got.ID != "work" || got.Label != "work" || got.DataDir != "/data/work" || got.Implicit {
		t.Fatalf("second profile = %+v", got)
	}
}

func TestResolveProfiles_InvalidEntriesCannotCaptureAnotherDataDir(t *testing.T) {
	home := "/home/u"
	profiles := ResolveProfiles([]ProfileSpec{{ID: "work", DataDir: "relative"}}, map[string]string{}, home)

	if len(profiles) != 1 || profiles[0].Implicit {
		t.Fatalf("profiles = %+v, want non-implicit fallback default", profiles)
	}
	if got := profiles[0].DataDir; got != filepath.Join(home, ".local", "share", "opencode") {
		t.Fatalf("fallback data dir = %q", got)
	}
}

func TestResolveProfiles_RejectsDuplicateDataDirsAndIDs(t *testing.T) {
	profiles := ResolveProfiles([]ProfileSpec{
		{ID: "personal", DataDir: "/data/personal"},
		{ID: "personal", DataDir: "/data/work"},
		{ID: "work", DataDir: "/data/personal"},
		{ID: "other", DataDir: "/data/other"},
	}, map[string]string{}, "/home/u")

	if len(profiles) != 2 || profiles[0].ID != "personal" || profiles[1].ID != "other" {
		t.Fatalf("profiles = %+v", profiles)
	}
}
