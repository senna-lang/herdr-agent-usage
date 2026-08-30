/**
 * Tests for session cache hit rate and remaining TTL presentation.
 *
 * TTL is published only from a recorded expiry. A guessed 5m/1h default
 * must not produce a countdown.
 */
package core

import "testing"

func TestCacheFromTokenCounts_HitPercent(t *testing.T) {
	got := CacheFromTokenCounts(100, 800, 100)
	if got == nil {
		t.Fatal("expected cache")
	}
	if got.FreshInputTokens != 100 || got.ReadTokens != 800 || got.CreationTokens != 100 {
		t.Fatalf("counters %+v", got)
	}
	if got.HitPercent != 80 {
		t.Fatalf("hit=%v want 80", got.HitPercent)
	}
}

func TestCacheFromTokenCounts_ZeroTotalIsNil(t *testing.T) {
	if CacheFromTokenCounts(0, 0, 0) != nil {
		t.Fatal("zero counters must not invent a cache row")
	}
}

func TestCacheFromTokenCounts_NegativeClamped(t *testing.T) {
	got := CacheFromTokenCounts(-10, 50, -1)
	if got == nil || got.FreshInputTokens != 0 || got.CreationTokens != 0 || got.ReadTokens != 50 {
		t.Fatalf("%+v", got)
	}
	if got.HitPercent != 100 {
		t.Fatalf("hit=%v want 100", got.HitPercent)
	}
}

func TestRemainingTTLSeconds_PrefersExpiresAt(t *testing.T) {
	expires := int64(1_700_003_600)
	ttl := int64(300)
	last := int64(1_700_000_000)
	cache := CacheUsage{ExpiresAtUnix: &expires, TTLSeconds: &ttl, LastActivityUnix: &last}
	got := RemainingTTLSeconds(cache, 1_700_000_000)
	if got == nil || *got != 3600 {
		t.Fatalf("got %#v want 3600 from expires_at", got)
	}
}

func TestRemainingTTLSeconds_RecordedBucket(t *testing.T) {
	ttl := int64(3600)
	last := int64(1_700_000_000)
	cache := CacheUsage{TTLSeconds: &ttl, LastActivityUnix: &last}
	got := RemainingTTLSeconds(cache, 1_700_000_120)
	if got == nil || *got != 3480 {
		t.Fatalf("got %#v want 3480", got)
	}
}

func TestRemainingTTLSeconds_ExpiredIsZero(t *testing.T) {
	expires := int64(1_700_000_000)
	cache := CacheUsage{ExpiresAtUnix: &expires}
	got := RemainingTTLSeconds(cache, 1_700_000_001)
	if got == nil || *got != 0 {
		t.Fatalf("got %#v want 0", got)
	}
}

func TestRemainingTTLSeconds_MissingEvidenceIsNil(t *testing.T) {
	if RemainingTTLSeconds(CacheUsage{}, 1) != nil {
		t.Fatal("no recorded expiry must not invent a TTL")
	}
	ttl := int64(3600)
	if RemainingTTLSeconds(CacheUsage{TTLSeconds: &ttl}, 1) != nil {
		t.Fatal("ttl without last activity must not invent a countdown")
	}
}

func TestFormatCacheStatus_HitAndTTL(t *testing.T) {
	expires := int64(1_700_003_480)
	cache := CacheUsage{HitPercent: 99.6, ExpiresAtUnix: &expires}
	got := FormatCacheStatus(cache, 1_700_000_000)
	if got != "cache reuse 99.6% · ttl≈58m" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatCacheStatus_HitOnlyWhenTTLUnknown(t *testing.T) {
	got := FormatCacheStatus(CacheUsage{HitPercent: 80}, 1)
	if got != "cache reuse 80.0%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatCacheStatus_ExpiredTTLWarns(t *testing.T) {
	expires := int64(1_000)
	got := FormatCacheStatus(CacheUsage{HitPercent: 50, ExpiresAtUnix: &expires}, 2_000)
	if got != "⚠️ cache reuse 50.0%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatCacheStatus_UnknownTTLDoesNotWarn(t *testing.T) {
	got := FormatCacheStatus(CacheUsage{HitPercent: 10}, 2_000)
	if got != "cache reuse 10.0%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatCacheStatus_HourTTL(t *testing.T) {
	expires := int64(1_700_003_600)
	got := FormatCacheStatus(CacheUsage{HitPercent: 10, ExpiresAtUnix: &expires}, 1_700_000_000)
	if got != "cache reuse 10.0% · ttl≈1h" {
		t.Fatalf("got %q", got)
	}
}
