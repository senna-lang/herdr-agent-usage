/**
 * Tests for ParseSidebarWidthFromToml / ResolveSidebarWidth.
 */
package core

import "testing"

func TestParseSidebarWidthFromToml_ReadsUI(t *testing.T) {
	toml := `
[theme]
name = "tokyo-night"

[ui]
agent_panel_sort = "spaces"
sidebar_width = 32
`
	got := ParseSidebarWidthFromToml(toml)
	if got == nil || *got != 32 {
		t.Fatalf("got %#v, want 32", got)
	}
}

func TestParseSidebarWidthFromToml_IgnoresComments(t *testing.T) {
	toml := `
[ui]
# sidebar_width = 32
agent_panel_sort = "spaces"
`
	if got := ParseSidebarWidthFromToml(toml); got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}

func TestParseSidebarWidthFromToml_IgnoresUIToast(t *testing.T) {
	toml := `
[ui]
agent_panel_sort = "spaces"

[ui.toast]
delay_seconds = 1
`
	if got := ParseSidebarWidthFromToml(toml); got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}

func TestDefaultSidebarWidth(t *testing.T) {
	if DefaultSidebarWidth != 26 {
		t.Fatalf("DefaultSidebarWidth = %d, want 26", DefaultSidebarWidth)
	}
}

func TestResolveSidebarWidth_PrefersLive(t *testing.T) {
	live := 21
	if got := ResolveSidebarWidth(&live, 32); got != 21 {
		t.Fatalf("got %d, want 21", got)
	}
}

func TestResolveSidebarWidth_FallsBackToConfigThenDefault(t *testing.T) {
	if got := ResolveSidebarWidth(nil, 0); got <= 0 {
		t.Fatalf("got %d, want > 0", got)
	}
	if got := ResolveSidebarWidth(nil, 0); got != DefaultSidebarWidth {
		t.Fatalf("got %d, want default %d", got, DefaultSidebarWidth)
	}
	if got := ResolveSidebarWidth(nil, 40); got != 40 {
		t.Fatalf("got %d, want 40", got)
	}
}
