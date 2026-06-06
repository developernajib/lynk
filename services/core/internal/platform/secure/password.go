package secure

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Password hashing: argon2id by default (the current OWASP first choice,
// memory-hard against GPU cracking), with bcrypt verification kept so a
// project migrating an existing user table keeps working. New hashes are
// always argon2id; bcrypt hashes verify until their users next change
// passwords.

// argon2id parameters, chosen against the OWASP password-storage cheat sheet:
// 64 MiB memory, 3 passes, 2 lanes, 16-byte salt, 32-byte key. Auth is not a
// hot path; the cost is the point.
const (
	argonMemoryKiB = 64 * 1024
	argonPasses    = 3
	argonLanes     = 2
	argonSaltLen   = 16
	argonKeyLen    = 32
)

// ErrUnknownHashFormat is returned when a stored hash matches no supported
// scheme, which usually means a corrupted column or an unsupported import.
var ErrUnknownHashFormat = errors.New("secure: unknown password hash format")

// HashPassword returns an argon2id hash in the standard PHC string format
// ($argon2id$v=19$m=...,t=...,p=...$salt$hash), self-describing so parameters
// can be raised later without breaking stored hashes.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("secure: read salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonPasses, argonMemoryKiB, argonLanes, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonPasses, argonLanes,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the stored hash. It
// dispatches on the hash's own format: argon2id (PHC) or bcrypt ($2a/$2b/$2y
// prefixes). Comparison is constant-time so timing reveals nothing.
func VerifyPassword(stored, password string) (bool, error) {
	switch {
	case strings.HasPrefix(stored, "$argon2id$"):
		return verifyArgon2id(stored, password)
	case strings.HasPrefix(stored, "$2a$"), strings.HasPrefix(stored, "$2b$"), strings.HasPrefix(stored, "$2y$"):
		err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password))
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("secure: bcrypt compare: %w", err)
		}
		return true, nil
	default:
		return false, ErrUnknownHashFormat
	}
}

// verifyArgon2id re-derives the key with the parameters stored in the hash
// itself, so older hashes verify even after the package defaults change.
func verifyArgon2id(stored, password string) (bool, error) {
	parts := strings.Split(stored, "$")
	// Expected: "", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash.
	if len(parts) != 6 {
		return false, ErrUnknownHashFormat
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrUnknownHashFormat
	}

	var memoryKiB, passes uint32
	var lanes uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memoryKiB, &passes, &lanes); err != nil {
		return false, ErrUnknownHashFormat
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrUnknownHashFormat
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrUnknownHashFormat
	}

	key := argon2.IDKey([]byte(password), salt, passes, memoryKiB, lanes, uint32(len(expected))) //nolint:gosec // key length is small

	return subtle.ConstantTimeCompare(key, expected) == 1, nil
}
