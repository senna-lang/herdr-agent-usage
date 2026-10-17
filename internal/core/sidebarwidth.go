/**
 * Resolves the sidebar width in columns.
 *
 * Priority:
 * 1. The live width passed in by the caller (pane layout's area.x)
 * 2. config.toml's ui.sidebar_width (caller supplies the parsed value)
 * 3. Herdr's default of 26
 *
 * area.x is the sidebar width only where Herdr lays the terminal area out to
 * the right of the sidebar (0.8.x source), so it follows a resized sidebar
 * there. Herdr 0.9.3 returns area.x = 0, which the caller treats as unknown,
 * so config or the default applies. It is not a documented width contract.
 *
 * File I/O for reading config is intentionally out of this pure package surface.
 */
package core

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultSidebarWidth is Herdr's default expanded sidebar width in columns.
const DefaultSidebarWidth = 26

// ParseSidebarWidthFromToml lightly extracts sidebar_width = N inside the [ui] block.
// Avoids a full TOML parser and matches the default Herdr comment style.
func ParseSidebarWidthFromToml(toml string) *int {
	inUI := false
	for _, rawLine := range strings.Split(toml, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			// Subtables like [ui.toast] are inside ui, but sidebar_width only
			// lives directly under [ui].
			if line == "[ui]" {
				inUI = true
			} else {
				inUI = false
			}
			continue
		}
		if !inUI {
			continue
		}
		const prefix = "sidebar_width"
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := strings.TrimSpace(line[len(prefix):])
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		numStr := strings.TrimSpace(rest[1:])
		n, err := strconv.Atoi(numStr)
		if err != nil || n <= 0 {
			continue
		}
		return &n
	}
	return nil
}

// ResolveSidebarWidth uses the live width when available, otherwise falls back
// to configWidth or DefaultSidebarWidth. configWidth <= 0 means "unset". The
// live width is a best-effort hint that Herdr 0.9+ no longer provides.
func ResolveSidebarWidth(liveWidth *int, configWidth int) int {
	if liveWidth != nil && *liveWidth > 0 {
		return *liveWidth
	}
	if configWidth > 0 {
		return configWidth
	}
	return DefaultSidebarWidth
}

// ResolveConfigSidebarWidth reads ui.sidebar_width from Herdr config, or default.
func ResolveConfigSidebarWidth() int {
	path := os.Getenv("HERDR_CONFIG_PATH")
	if path == "" {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			path = filepath.Join(xdg, "herdr", "config.toml")
		} else {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, ".config", "herdr", "config.toml")
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return DefaultSidebarWidth
	}
	if n := ParseSidebarWidthFromToml(string(raw)); n != nil {
		return *n
	}
	return DefaultSidebarWidth
}
