// Package vo holds the identity module's value objects.
package vo

import (
	"errors"
	"net/mail"
	"strings"
)

// ErrInvalidEmail rejects malformed addresses at construction.
var ErrInvalidEmail = errors.New("invalid email address")

// Email is a validated, canonicalized (lowercase) email address. Lowercasing
// at the boundary means lookups and the unique index agree on one spelling.
type Email string

// NewEmail validates with the stdlib RFC 5322 parser and canonicalizes.
func NewEmail(raw string) (Email, error) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	addr, err := mail.ParseAddress(trimmed)
	if err != nil || addr.Address != trimmed {
		return "", ErrInvalidEmail
	}
	return Email(trimmed), nil
}

// String returns the canonical address.
func (e Email) String() string { return string(e) }
