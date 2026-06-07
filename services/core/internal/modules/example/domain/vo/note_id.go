// Package vo holds the example module's value objects: small immutable types
// that validate at construction so an invalid value cannot exist anywhere in
// the domain.
package vo

import "errors"

// ErrInvalidNoteID rejects malformed ids at the boundary.
var ErrInvalidNoteID = errors.New("invalid note id")

// NoteID is the note's identity. Minting (UUIDv7) happens in the application
// layer through a generator port; the domain only validates shape, keeping it
// stdlib-only.
type NoteID string

// NewNoteID validates a raw id. The check is shape-level (canonical UUID
// length and dashes); cryptographic uniqueness is the generator's job.
func NewNoteID(raw string) (NoteID, error) {
	if len(raw) != 36 || raw[8] != '-' || raw[13] != '-' || raw[18] != '-' || raw[23] != '-' {
		return "", ErrInvalidNoteID
	}
	return NoteID(raw), nil
}

// String returns the canonical text form.
func (id NoteID) String() string { return string(id) }
