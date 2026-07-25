/**
 * Tests for the opencode.ai usage fetch cache TTL rules.
 */
package limits

import "testing"

func TestOpenCodeWebCacheFresh(t *testing.T) {
	const now = 1784946198000
	limits := &ProviderLimits{ProviderID: "opencode"}

	for _, tc := range []struct {
		name  string
		entry openCodeWebCacheEntry
		want  bool
	}{
		{"fresh success", openCodeWebCacheEntry{FetchedAtMs: now - 1000, Outcome: openCodeWebFetched, Limits: limits}, true},
		{"expired success", openCodeWebCacheEntry{FetchedAtMs: now - openCodeWebCacheSuccessTTLMs, Outcome: openCodeWebFetched, Limits: limits}, false},
		{"success without limits", openCodeWebCacheEntry{FetchedAtMs: now - 1000, Outcome: openCodeWebFetched}, false},
		{"fresh fetch failure", openCodeWebCacheEntry{FetchedAtMs: now - openCodeWebCacheSuccessTTLMs - 1, Outcome: openCodeWebFetchFailed}, true},
		{"expired fetch failure", openCodeWebCacheEntry{FetchedAtMs: now - openCodeWebCacheFailureTTLMs, Outcome: openCodeWebFetchFailed}, false},
		{"fresh no-session", openCodeWebCacheEntry{FetchedAtMs: now - 1000, Outcome: openCodeWebNoSession}, true},
		// A sign-in must take effect promptly, so no-session backs off far less
		// than a genuine fetch failure.
		{"expired no-session", openCodeWebCacheEntry{FetchedAtMs: now - openCodeWebCacheNoSessionTTLMs, Outcome: openCodeWebNoSession}, false},
		{"unset", openCodeWebCacheEntry{}, false},
		{"missing outcome", openCodeWebCacheEntry{FetchedAtMs: now - 1000, Limits: limits}, false},
		{"future stamp", openCodeWebCacheEntry{FetchedAtMs: now + 1000, Outcome: openCodeWebFetched, Limits: limits}, false},
	} {
		if got := openCodeWebCacheFresh(tc.entry, now); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestOpenCodeWebCacheNoSessionBacksOffLessThanFailure(t *testing.T) {
	if openCodeWebCacheTTLMs(openCodeWebNoSession) >= openCodeWebCacheTTLMs(openCodeWebFetchFailed) {
		t.Fatalf("no-session must retry sooner than a fetch failure")
	}
}

// A cached unsuccessful attempt must keep the network out of the path while
// still letting the caller fall back to the local estimate.
func TestOpenCodeWebCacheRoundTrip(t *testing.T) {
	const now = 1784946198000
	t.Setenv("USAGEBAR_OPENCODE_WEB_CACHE_PATH", t.TempDir()+"/opencode-go-web.json")

	if _, fresh := loadOpenCodeWebCache(now); fresh {
		t.Fatalf("missing cache file should be a miss")
	}

	want := ProviderLimits{ProviderID: "opencode", Label: "OpenCode", Source: "opencode.ai web"}
	saveOpenCodeWebCache(&want, openCodeWebFetched, now)
	got, fresh := loadOpenCodeWebCache(now + 1000)
	if !fresh || got == nil {
		t.Fatalf("expected a cache hit, got fresh=%v limits=%v", fresh, got)
	}
	if got.Source != want.Source {
		t.Fatalf("Source: got %q want %q", got.Source, want.Source)
	}
	if _, fresh := loadOpenCodeWebCache(now + openCodeWebCacheSuccessTTLMs); fresh {
		t.Fatalf("entry should expire after the success TTL")
	}

	saveOpenCodeWebCache(nil, openCodeWebFetchFailed, now)
	got, fresh = loadOpenCodeWebCache(now + 1000)
	if !fresh {
		t.Fatalf("cached failure should be fresh")
	}
	if got != nil {
		t.Fatalf("cached failure should carry no limits, got %v", got)
	}

	saveOpenCodeWebCache(nil, openCodeWebNoSession, now)
	if _, fresh := loadOpenCodeWebCache(now + openCodeWebCacheNoSessionTTLMs + 1); fresh {
		t.Fatalf("no-session entry should expire after its own TTL")
	}
}
