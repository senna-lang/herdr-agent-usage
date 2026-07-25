/**
 * Tests for Chromium cookie decryption helpers.
 */
package limits

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"testing"
)

// encryptChromiumValue mirrors Chromium's macOS v10 encryption so the
// decrypt path can be exercised without a real browser profile.
func encryptChromiumValue(t *testing.T, key []byte, hostKey, value string, bindDomain bool) []byte {
	t.Helper()
	plain := []byte(value)
	if bindDomain {
		sum := sha256.Sum256([]byte(hostKey))
		plain = append(sum[:], plain...)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, chromiumIV()).CryptBlocks(out, plain)
	return append([]byte(chromiumV10Prefix), out...)
}

func TestDeriveChromiumKey(t *testing.T) {
	key, err := DeriveChromiumKey("safe-storage-password")
	if err != nil {
		t.Fatalf("DeriveChromiumKey: %v", err)
	}
	if len(key) != chromiumKeyLength {
		t.Fatalf("key length: got %d want %d", len(key), chromiumKeyLength)
	}
	again, err := DeriveChromiumKey("safe-storage-password")
	if err != nil || !bytes.Equal(key, again) {
		t.Fatalf("derivation is not deterministic")
	}
	other, err := DeriveChromiumKey("different")
	if err != nil {
		t.Fatalf("DeriveChromiumKey(other): %v", err)
	}
	if bytes.Equal(key, other) {
		t.Fatalf("different passwords produced the same key")
	}
	if _, err := DeriveChromiumKey(""); err == nil {
		t.Fatalf("empty password should fail")
	}
}

func TestDecryptChromiumCookieValueRoundTrip(t *testing.T) {
	key, err := DeriveChromiumKey("safe-storage-password")
	if err != nil {
		t.Fatalf("DeriveChromiumKey: %v", err)
	}
	for _, tc := range []struct {
		name       string
		bindDomain bool
	}{
		{"legacy value", false},
		{"domain-bound value (Chromium 127+)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enc := encryptChromiumValue(t, key, ".opencode.ai", "session-token-abc123", tc.bindDomain)
			got, ok := DecryptChromiumCookieValue(key, ".opencode.ai", enc)
			if !ok {
				t.Fatalf("decrypt failed")
			}
			if got != "session-token-abc123" {
				t.Fatalf("got %q want %q", got, "session-token-abc123")
			}
		})
	}
}

func TestDecryptChromiumCookieValueRejects(t *testing.T) {
	key, _ := DeriveChromiumKey("safe-storage-password")
	wrong, _ := DeriveChromiumKey("wrong-password")
	enc := encryptChromiumValue(t, key, ".opencode.ai", "session-token-abc123", true)

	if _, ok := DecryptChromiumCookieValue(wrong, ".opencode.ai", enc); ok {
		t.Fatalf("wrong key should not decrypt")
	}
	if _, ok := DecryptChromiumCookieValue(key, ".opencode.ai", []byte("plain value")); ok {
		t.Fatalf("non-v10 blob should be rejected")
	}
	if _, ok := DecryptChromiumCookieValue(key, ".opencode.ai", nil); ok {
		t.Fatalf("empty blob should be rejected")
	}
	if _, ok := DecryptChromiumCookieValue(key[:8], ".opencode.ai", enc); ok {
		t.Fatalf("short key should be rejected")
	}
	if _, ok := DecryptChromiumCookieValue(key, ".opencode.ai", append([]byte(chromiumV10Prefix), 1, 2, 3)); ok {
		t.Fatalf("non-block-aligned body should be rejected")
	}
}

// A value whose plaintext happens to start with 32 bytes that are not the
// domain hash must survive intact.
func TestDecryptChromiumCookieValueKeepsUnmatchedPrefix(t *testing.T) {
	key, _ := DeriveChromiumKey("safe-storage-password")
	long := "0123456789abcdef0123456789abcdef-tail"
	enc := encryptChromiumValue(t, key, ".opencode.ai", long, false)
	got, ok := DecryptChromiumCookieValue(key, ".opencode.ai", enc)
	if !ok || got != long {
		t.Fatalf("got %q ok=%v want %q", got, ok, long)
	}
}

func TestCookieHostMatchesDomain(t *testing.T) {
	for _, tc := range []struct {
		host, domain string
		want         bool
	}{
		{"opencode.ai", "opencode.ai", true},
		{".opencode.ai", "opencode.ai", true},
		{"app.opencode.ai", "opencode.ai", true},
		{".app.opencode.ai", "opencode.ai", true},
		{"OpenCode.ai", "opencode.ai", true},
		{"notopencode.ai", "opencode.ai", false},
		{"opencode.ai.evil.com", "opencode.ai", false},
		{"", "opencode.ai", false},
		{"opencode.ai", "", false},
	} {
		if got := CookieHostMatchesDomain(tc.host, tc.domain); got != tc.want {
			t.Fatalf("CookieHostMatchesDomain(%q,%q) = %v want %v", tc.host, tc.domain, got, tc.want)
		}
	}
}

func TestChromiumCookieExpired(t *testing.T) {
	const nowMs = 1784946198000
	const epochOffsetMicros = 11644473600000000
	future := (nowMs+3600_000)*1000 + epochOffsetMicros
	past := (nowMs-3600_000)*1000 + epochOffsetMicros

	if ChromiumCookieExpired(0, nowMs) {
		t.Fatalf("session cookie should not be expired")
	}
	if ChromiumCookieExpired(int64(future), nowMs) {
		t.Fatalf("future expiry should not be expired")
	}
	if !ChromiumCookieExpired(int64(past), nowMs) {
		t.Fatalf("past expiry should be expired")
	}
}

func TestBuildCookieHeader(t *testing.T) {
	got := BuildCookieHeader([]BrowserCookie{
		{HostKey: "opencode.ai", Name: "session", Value: "a"},
		{HostKey: ".opencode.ai", Name: "session", Value: "duplicate-ignored"},
		{HostKey: ".opencode.ai", Name: "", Value: "no-name"},
		{HostKey: ".opencode.ai", Name: "empty", Value: ""},
		{HostKey: ".opencode.ai", Name: "csrf", Value: "b"},
	})
	if got != "session=a; csrf=b" {
		t.Fatalf("got %q", got)
	}
	if BuildCookieHeader(nil) != "" {
		t.Fatalf("empty input should produce an empty header")
	}
}
