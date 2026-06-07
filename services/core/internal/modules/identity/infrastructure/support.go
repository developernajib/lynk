// Package infrastructure implements the identity module's ports.
package infrastructure

import (
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/developernajib/lynk/services/core/internal/platform/secure"
)

// Modules never import each other, so each carries its own copy of these
// small helpers; the duplication is the price of mechanical extractability.

func uuidFromString(raw string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(raw); err != nil {
		return pgtype.UUID{}, fmt.Errorf("identity: parse uuid %q: %w", raw, err)
	}
	return u, nil
}

func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	var s [36]byte
	hex.Encode(s[:8], u.Bytes[:4])
	s[8] = '-'
	hex.Encode(s[9:13], u.Bytes[4:6])
	s[13] = '-'
	hex.Encode(s[14:18], u.Bytes[6:8])
	s[18] = '-'
	hex.Encode(s[19:23], u.Bytes[8:10])
	s[23] = '-'
	hex.Encode(s[24:], u.Bytes[10:])
	return string(s[:])
}

// UUIDGenerator adapts secure.UUIDv7 to the IDGenerator port.
type UUIDGenerator struct{}

// NewID mints a time-ordered UUIDv7.
func (UUIDGenerator) NewID() (string, error) { return secure.UUIDv7() }
