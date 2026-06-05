package apperror

import (
	"crypto/sha256"
	"encoding/hex"
)

// Fingerprint returns a short, stable hash identifying this class of error,
// suitable for grouping occurrences in a log aggregator (Sentry-style issue
// triage without a separate service).
//
// Only Kind and Code participate: the dynamic Message and cause vary per
// request and would scatter one logical bug across many groups.
func (e *Error) Fingerprint() string {
	h := sha256.New()
	// Write never fails on a hash.Hash; the error is io.Writer ceremony.
	_, _ = h.Write([]byte(e.Kind.String()))
	_, _ = h.Write([]byte{0}) // separator so "ab"+"c" != "a"+"bc"
	_, _ = h.Write([]byte(e.Code))

	// 8 bytes (16 hex chars) reads well on a dashboard and is wide enough to
	// avoid practical collisions across an error catalog.
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:8])
}
