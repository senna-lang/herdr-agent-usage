/**
 * Tests for periodic session-cache fan-out onto sidebar $cache tokens.
 */
package update

import (
	"testing"
	"time"

	"github.com/senna-lang/herdr-agent-usage/internal/core"
	"github.com/senna-lang/herdr-agent-usage/internal/herdrcli"
	"github.com/senna-lang/herdr-agent-usage/internal/limits"
)

func TestPublishOpenPaneCachesWith_WritesCacheOnly(t *testing.T) {
	agent := "claude"
	status := "idle"
	expires := int64(1_700_000_300)
	pane := herdrcli.PaneInfo{
		Agent:       &agent,
		AgentStatus: &status,
		Tokens: map[string]string{
			"limit":   "5h 72%",
			"context": "⛁ 13% (130k)",
		},
	}
	written := map[string]string{}
	writer := metadataTokenWriter{
		set: func(_, _, name, value string) bool {
			written[name] = value
			return true
		},
		clear: func(_, _, name string) bool {
			written[name] = ""
			return true
		},
	}

	publishOpenPaneCachesWith(
		writer,
		func() ([]limits.OpenPaneSnapshot, bool) {
			return []limits.OpenPaneSnapshot{{PaneID: "p1", Agent: agent}}, true
		},
		func(string) herdrcli.PaneInfo { return pane },
		func(limits.OpenPaneSnapshot) (string, bool) { return "claude", true },
		func(string, herdrcli.PaneInfo, string) *core.ContextUsage {
			return &core.ContextUsage{Cache: &core.CacheUsage{HitPercent: 80, ExpiresAtUnix: &expires}}
		},
		time.Unix(1_700_000_000, 0),
	)

	if got := written["cache"]; got != "cache reuse 80.0% · ttl≈5m" {
		t.Fatalf("cache=%q", got)
	}
	if _, ok := written["limit"]; ok {
		t.Fatalf("cache publisher changed $limit: %v", written)
	}
	if _, ok := written["context"]; ok {
		t.Fatalf("cache publisher changed $context: %v", written)
	}
}

func TestPublishOpenPaneCachesWith_ClearsUnresolvedSettledCache(t *testing.T) {
	agent := "claude"
	status := "idle"
	pane := herdrcli.PaneInfo{
		Agent:       &agent,
		AgentStatus: &status,
		Tokens:      map[string]string{"cache": "cache reuse 99.6% · ttl≈5m"},
	}
	cleared := false
	writer := metadataTokenWriter{
		set:   func(_, _, _, _ string) bool { return true },
		clear: func(_, _, name string) bool { cleared = name == "cache"; return true },
	}

	publishOpenPaneCachesWith(
		writer,
		func() ([]limits.OpenPaneSnapshot, bool) {
			return []limits.OpenPaneSnapshot{{PaneID: "p1", Agent: agent}}, true
		},
		func(string) herdrcli.PaneInfo { return pane },
		func(limits.OpenPaneSnapshot) (string, bool) { return "", false },
		func(string, herdrcli.PaneInfo, string) *core.ContextUsage {
			t.Fatal("unresolved pane must not resolve usage")
			return nil
		},
		time.Unix(0, 0),
	)
	if !cleared {
		t.Fatal("unresolved settled pane must clear stale $cache")
	}
}

func TestPublishOpenPaneCachesWith_RetainsWorkingCacheWithoutUsage(t *testing.T) {
	agent := "claude"
	status := "working"
	pane := herdrcli.PaneInfo{
		Agent:       &agent,
		AgentStatus: &status,
		Tokens:      map[string]string{"cache": "cache reuse 99.6% · ttl≈5m"},
	}
	wrote := false
	writer := metadataTokenWriter{
		set:   func(_, _, _, _ string) bool { wrote = true; return true },
		clear: func(_, _, _ string) bool { wrote = true; return true },
	}

	publishOpenPaneCachesWith(
		writer,
		func() ([]limits.OpenPaneSnapshot, bool) {
			return []limits.OpenPaneSnapshot{{PaneID: "p1", Agent: agent}}, true
		},
		func(string) herdrcli.PaneInfo { return pane },
		func(limits.OpenPaneSnapshot) (string, bool) { return "claude", true },
		func(string, herdrcli.PaneInfo, string) *core.ContextUsage { return nil },
		time.Unix(0, 0),
	)
	if wrote {
		t.Fatal("working pane must retain its last-known-good $cache")
	}
}
