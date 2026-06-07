package secure

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashToken returns the hex-encoded SHA-256 of a raw token, for storing
// opaque tokens (refresh tokens, API keys) that must be looked up by value.
//
// SHA-256 rather than a password KDF on purpose: these tokens are already
// high-entropy crypto/rand output, so they need no salt or slow hash, and a
// deterministic digest is what makes a by-hash lookup possible. Passwords are
// low-entropy and take the opposite treatment (salted, slow KDF).
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
