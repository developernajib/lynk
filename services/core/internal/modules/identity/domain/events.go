package domain

import "time"

// Event is a fact the module records for the outside world, published
// through the transactional outbox. Events carry NO secrets: never a
// password, hash, or token.
type Event interface {
	// Subject is the bus routing key.
	Subject() string
}

// UserRegistered is raised once per new account; downstream consumers
// (welcome email, analytics) project from it without calling back.
type UserRegistered struct {
	UserID     string
	Email      string
	FullName   string
	Role       string
	OccurredAt time.Time
}

// Subject implements Event.
func (UserRegistered) Subject() string { return "identity.user.registered" }

// PasswordChanged is raised so security-notification consumers can alert the
// account owner.
type PasswordChanged struct {
	UserID     string
	OccurredAt time.Time
}

// Subject implements Event.
func (PasswordChanged) Subject() string { return "identity.user.password_changed" }

// RoleChanged is raised when an admin reassigns a user's role, a
// security-relevant fact the audit ledger must capture.
type RoleChanged struct {
	UserID     string
	Role       string
	OccurredAt time.Time
}

// Subject implements Event.
func (RoleChanged) Subject() string { return "identity.user.role_changed" }

// PasswordResetRequested is raised when a reset code is issued. It carries
// NO code; delivery goes through the notifier port, never the bus.
type PasswordResetRequested struct {
	UserID     string
	OccurredAt time.Time
}

// Subject implements Event.
func (PasswordResetRequested) Subject() string { return "identity.user.password_reset_requested" }

// EmailVerified is raised when a user proves their address.
type EmailVerified struct {
	UserID     string
	OccurredAt time.Time
}

// Subject implements Event.
func (EmailVerified) Subject() string { return "identity.user.email_verified" }

// APIKeyCreated is raised when a machine credential is minted.
type APIKeyCreated struct {
	UserID     string
	KeyID      string
	Name       string
	OccurredAt time.Time
}

// Subject implements Event.
func (APIKeyCreated) Subject() string { return "identity.apikey.created" }

// APIKeyRevoked is raised when a machine credential is disabled.
type APIKeyRevoked struct {
	UserID     string
	KeyID      string
	OccurredAt time.Time
}

// Subject implements Event.
func (APIKeyRevoked) Subject() string { return "identity.apikey.revoked" }
