package pathutil

import (
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	for _, tt := range []struct {
		name string
		in   string
		want string
	}{
		{name: "bare tilde", in: "~", want: home},
		{name: "slash form", in: "~/.claude", want: filepath.Join(home, ".claude")},
		{name: "absolute path unchanged", in: filepath.Join(home, "x"), want: filepath.Join(home, "x")},
		{name: "named user form unchanged", in: "~other/.claude", want: "~other/.claude"},
		{name: "empty unchanged", in: "", want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExpandHome(tt.in, home); got != tt.want {
				t.Fatalf("ExpandHome(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExpandHome_EmptyHomeLeavesPathUnchanged(t *testing.T) {
	if got := ExpandHome("~/.claude", ""); got != "~/.claude" {
		t.Fatalf("got %q", got)
	}
}

// Windows users naturally write "~\.claude"; on Unix a backslash is an
// ordinary file-name character, so the same string must stay literal there.
func TestExpandHome_BackslashFormFollowsPlatformSeparator(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	got := ExpandHome(`~\.claude`, home)
	want := `~\.claude`
	if filepath.Separator == '\\' {
		want = filepath.Join(home, ".claude")
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
