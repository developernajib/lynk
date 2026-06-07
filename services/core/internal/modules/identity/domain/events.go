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
