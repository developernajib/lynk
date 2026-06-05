package secure

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"
)

// UUIDv7 returns a new RFC 9562 version 7 UUID string.
//
// Version 7 leads with a 48-bit Unix millisecond timestamp, so values sort by
// creation time: as primary keys they append to the right side of a B-tree
// index instead of scattering like UUIDv4. The remaining 74 bits come from
// crypto/rand. Implemented on the stdlib to keep the core dependency-free.
func UUIDv7() (string, error) {
	var u [16]byte
	if _, err := rand.Read(u[6:]); err != nil {
		return "", fmt.Errorf("secure: read random: %w", err)
	}

	// Place the 48-bit timestamp in the first 6 bytes, big-endian.
	ms := uint64(time.Now().UnixMilli()) //nolint:gosec // never negative after 1970
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], ms<<16)
	copy(u[:6], ts[:6])

	u[6] = (u[6] & 0x0f) | 0x70 // version 7
	u[8] = (u[8] & 0x3f) | 0x80 // RFC 9562 variant

	// Standard 8-4-4-4-12 text form.
	var s [36]byte
	hex.Encode(s[:8], u[:4])
	s[8] = '-'
	hex.Encode(s[9:13], u[4:6])
	s[13] = '-'
	hex.Encode(s[14:18], u[6:8])
	s[18] = '-'
	hex.Encode(s[19:23], u[8:10])
	s[23] = '-'
	hex.Encode(s[24:], u[10:])
	return string(s[:]), nil
}
