/**
 * Decrypts Chromium-family cookie values (macOS "v10" scheme) so the
 * OpenCode Go collector can reuse a browser session instead of requiring a
 * hand-pasted OPENCODE_GO_COOKIE. Pure helpers only: profile discovery,
 * sqlite reads, and the Keychain lookup live in chromiumcookies_io.go.
 */
package limits

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"strings"
)

const (
	// chromiumV10Prefix marks a value encrypted with the Keychain-derived key.
	chromiumV10Prefix = "v10"
	// chromiumSalt/Iterations/KeyLength are Chromium's fixed macOS parameters
	// (components/os_crypt/sync/os_crypt_mac.mm).
	chromiumSalt       = "saltysalt"
	chromiumIterations = 1003
	chromiumKeyLength  = 16
)

// chromiumIV is Chromium's fixed CBC IV: 16 spaces.
func chromiumIV() []byte { return bytes.Repeat([]byte{' '}, aes.BlockSize) }

// BrowserCookie is one cookie row after decryption.
type BrowserCookie struct {
	HostKey string
	Name    string
	Value   string
}

// DeriveChromiumKey derives the AES-128 key from a browser's
// "<Browser> Safe Storage" Keychain password.
func DeriveChromiumKey(password string) ([]byte, error) {
	if password == "" {
		return nil, errors.New("empty safe storage password")
	}
	return pbkdf2.Key(sha1.New, password, []byte(chromiumSalt), chromiumIterations, chromiumKeyLength)
}

// DecryptChromiumCookieValue decrypts one `encrypted_value` blob. hostKey is
// needed because Chromium 127+ prefixes the plaintext with SHA256(host_key);
// the prefix is stripped only when it actually matches, so a wrong key can
// never silently truncate a real value. ok=false means the blob is not a v10
// value, the key is wrong, or the plaintext is not a usable cookie value —
// callers should fall back to the row's unencrypted `value` column.
func DecryptChromiumCookieValue(key []byte, hostKey string, encrypted []byte) (string, bool) {
	if len(key) != chromiumKeyLength || len(encrypted) <= len(chromiumV10Prefix) {
		return "", false
	}
	if !bytes.HasPrefix(encrypted, []byte(chromiumV10Prefix)) {
		return "", false
	}
	body := encrypted[len(chromiumV10Prefix):]
	if len(body) == 0 || len(body)%aes.BlockSize != 0 {
		return "", false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", false
	}
	plain := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, chromiumIV()).CryptBlocks(plain, body)
	plain, ok := stripPKCS7(plain)
	if !ok {
		return "", false
	}
	plain = stripChromiumDomainHash(plain, hostKey)
	value := string(plain)
	if !isPrintableCookieValue(value) {
		return "", false
	}
	return value, true
}

// stripPKCS7 removes CBC padding, rejecting malformed padding (which is the
// usual symptom of decrypting with the wrong key).
func stripPKCS7(plain []byte) ([]byte, bool) {
	if len(plain) == 0 || len(plain)%aes.BlockSize != 0 {
		return nil, false
	}
	pad := int(plain[len(plain)-1])
	if pad <= 0 || pad > aes.BlockSize || pad > len(plain) {
		return nil, false
	}
	for _, b := range plain[len(plain)-pad:] {
		if int(b) != pad {
			return nil, false
		}
	}
	return plain[:len(plain)-pad], true
}

// stripChromiumDomainHash removes the SHA256(host_key) prefix Chromium 127+
// binds to the cookie value. Older browsers store the bare value, so the
// prefix is removed only on an exact match.
func stripChromiumDomainHash(plain []byte, hostKey string) []byte {
	if len(plain) < sha256.Size {
		return plain
	}
	want := sha256.Sum256([]byte(hostKey))
	if !bytes.Equal(plain[:sha256.Size], want[:]) {
		return plain
	}
	return plain[sha256.Size:]
}

// isPrintableCookieValue rejects control characters, which appear when the
// derived key is wrong but the padding happens to validate.
func isPrintableCookieValue(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// CookieHostMatchesDomain reports whether a stored host_key covers domain or
// one of its subdomains (".opencode.ai" and "app.opencode.ai" both match
// "opencode.ai").
func CookieHostMatchesDomain(hostKey, domain string) bool {
	host := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(hostKey), "."))
	domain = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "."))
	if host == "" || domain == "" {
		return false
	}
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// ChromiumCookieExpired reports whether a row is past its expiry. expiresUTC
// is Chromium's microseconds-since-1601 stamp; 0 marks a session cookie,
// which never expires on disk.
func ChromiumCookieExpired(expiresUTC int64, nowMs int64) bool {
	if expiresUTC <= 0 {
		return false
	}
	const epochOffsetMicros = 11644473600000000
	return (expiresUTC-epochOffsetMicros)/1000 <= nowMs
}

// BuildCookieHeader joins cookies into a Cookie request header, keeping the
// first occurrence of each name so a host-exact row wins over a domain-wide
// duplicate when callers pass the more specific rows first.
func BuildCookieHeader(cookies []BrowserCookie) string {
	var parts []string
	seen := map[string]struct{}{}
	for _, c := range cookies {
		name := strings.TrimSpace(c.Name)
		if name == "" || c.Value == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		parts = append(parts, name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}
