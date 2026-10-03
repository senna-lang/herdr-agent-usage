/**
 * Opaque identity for a credential, used to scope cached readings.
 *
 * The input is hashed rather than stored: a cached limit row needs to know
 * *which account* produced it, and that must never require keeping the token
 * itself on disk. A different login produces a different id, which is exactly
 * what makes another account's cached reading refuseable.
 */
package kilo

import (
	"crypto/sha256"
	"encoding/hex"
)

// CredentialID returns a stable, non-reversible identifier for a token.
func CredentialID(secret string) string {
	sum := sha256.Sum256([]byte("kilo\000" + secret))
	return "kilo:" + hex.EncodeToString(sum[:])
}
