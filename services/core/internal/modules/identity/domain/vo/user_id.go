package vo

import "errors"

// ErrInvalidUserID rejects malformed ids.
var ErrInvalidUserID = errors.New("invalid user id")

// UserID is the user's identity. Minting (UUIDv7) happens in the
// application layer through a generator port; the domain validates shape.
type UserID string

// NewUserID validates the canonical UUID shape.
func NewUserID(raw string) (UserID, error) {
	if len(raw) != 36 || raw[8] != '-' || raw[13] != '-' || raw[18] != '-' || raw[23] != '-' {
		return "", ErrInvalidUserID
	}
	return UserID(raw), nil
}

// String returns the canonical text form.
func (id UserID) String() string { return string(id) }
