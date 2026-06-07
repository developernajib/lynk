package vo

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ErrInvalidTitle rejects empty or oversized titles.
var ErrInvalidTitle = errors.New("title must be 1-200 characters")

// Title is a validated, trimmed note title.
type Title string

// maxTitleLen counts runes, not bytes, so multi-byte scripts get the same
// budget as ASCII.
const maxTitleLen = 200

// NewTitle trims and validates.
func NewTitle(raw string) (Title, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxTitleLen {
		return "", ErrInvalidTitle
	}
	return Title(trimmed), nil
}

// String returns the title text.
func (t Title) String() string { return string(t) }
