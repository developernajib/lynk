// Package secure generates cryptographically strong random values: tokens,
// API keys, OTP codes, nonces, and UUIDs.
//
// Everything here draws from crypto/rand. Predictable secrets are forgeable,
// so math/rand and time-seeded generators are banned for anything
// security-sensitive; centralizing generation in one package keeps that rule
// enforceable.
package secure

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
)

// Token returns a URL-safe random string carrying n bytes of entropy.
// Callers specify entropy in bytes; the encoded string is longer.
func Token(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("secure: read random: %w", err)
	}
	// RawURLEncoding has no '+', '/', or '=' so the token needs no escaping
	// in URLs, headers, or cookies.
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HexToken returns a random lowercase hex string carrying n bytes of entropy,
// for contexts that want only [0-9a-f] such as JWT IDs.
func HexToken(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("secure: read random: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// NumericCode returns a zero-padded random decimal code with the given number
// of digits, for OTP and verification codes.
//
// rand.Int draws uniformly in [0, 10^digits), unlike a modulo over random
// bytes which would bias some codes.
func NumericCode(digits int) (string, error) {
	if digits <= 0 {
		return "", fmt.Errorf("secure: digits must be positive")
	}
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)
	v, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("secure: random int: %w", err)
	}
	return fmt.Sprintf("%0*d", digits, v), nil
}
