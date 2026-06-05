// Package apperror defines a transport-agnostic application error type.
//
// Domain code raises plain sentinel errors and stays ignorant of transport.
// Adapters classify those into an *Error carrying a Kind (mapped to a status
// code at the transport layer), a stable machine-readable Code, and a safe
// public Message. The wrapped cause stays unexported so it can never leak to
// clients, while errors.Is/As still walk the chain.
package apperror

import (
	"errors"
	"fmt"
)

// Kind is a closed set of error categories that map cleanly onto transport
// status codes without the lower layers importing any transport package.
type Kind int

const (
	// KindInternal is the zero value on purpose: an error nobody classified
	// is treated as a server fault, never as anything more permissive.
	KindInternal Kind = iota
	KindInvalidInput
	KindNotFound
	KindAlreadyExists
	KindUnauthenticated
	KindPermissionDenied
	// KindConflict signals an optimistic-locking or version conflict.
	KindConflict
	KindRateLimited
	// KindUnavailable signals a downstream dependency failure.
	KindUnavailable
)

// String returns a stable snake_case name used in logs and fingerprints.
func (k Kind) String() string {
	switch k {
	case KindInvalidInput:
		return "invalid_input"
	case KindNotFound:
		return "not_found"
	case KindAlreadyExists:
		return "already_exists"
	case KindUnauthenticated:
		return "unauthenticated"
	case KindPermissionDenied:
		return "permission_denied"
	case KindConflict:
		return "conflict"
	case KindRateLimited:
		return "rate_limited"
	case KindUnavailable:
		return "unavailable"
	default:
		return "internal"
	}
}

// Error is a classified application error.
type Error struct {
	// Kind categorizes the failure and drives the transport status code.
	Kind Kind
	// Code is a short, stable machine string clients can switch on,
	// e.g. "order_already_paid". It is part of the API contract.
	Code string
	// Message is safe to show externally. It must never contain secrets,
	// SQL, or stack details; those belong in logs only.
	Message string
	// cause is unexported so the underlying error is never serialized to a
	// client. It remains reachable through Unwrap.
	cause error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the wrapped cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.cause }

// New returns an *Error with no wrapped cause.
func New(kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

// Wrap returns an *Error around cause, preserving it for logging and error
// inspection while presenting a safe Kind, Code, and Message externally.
func Wrap(cause error, kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message, cause: cause}
}

// As reports whether err is or wraps an *Error, returning it when found.
func As(err error) (*Error, bool) {
	return errors.AsType[*Error](err)
}
