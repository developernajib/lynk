package domain

import (
	"errors"
	"time"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
)

// Status is the account lifecycle state.
type Status string

const (
	// StatusActive may sign in.
	StatusActive Status = "active"
	// StatusDisabled exists but may not sign in.
	StatusDisabled Status = "disabled"
)

// ErrMissingName guards the display-name invariant.
var ErrMissingName = errors.New("user requires a name")

// ErrInvalidRole rejects empty or oversized role names. Roles are an open
// set on purpose: ABAC policies give meaning to role strings at runtime.
var ErrInvalidRole = errors.New("role must be 1-50 characters")

// User is the aggregate root. The password hash lives here as an opaque
// string: HOW it is hashed is infrastructure; THAT a user has exactly one
// credential is domain.
type User struct {
	id           vo.UserID
	email        vo.Email
	passwordHash string
	fullName     string
	role         string
	status       Status
	version      int64
	createdAt    time.Time
	updatedAt    time.Time

	events []Event
}

// NewUser is the validating factory for registration. It records
// UserRegistered; persistence publishes it through the outbox atomically.
func NewUser(id vo.UserID, email vo.Email, passwordHash, fullName string, now time.Time) (*User, error) {
	if fullName == "" {
		return nil, ErrMissingName
	}

	u := &User{
		id:           id,
		email:        email,
		passwordHash: passwordHash,
		fullName:     fullName,
		role:         "user",
		status:       StatusActive,
		version:      1,
		createdAt:    now,
		updatedAt:    now,
	}
	u.events = append(u.events, UserRegistered{
		UserID:     id.String(),
		Email:      email.String(),
		FullName:   fullName,
		Role:       u.role,
		OccurredAt: now,
	})
	return u, nil
}

// UserFromState rehydrates without validation or events.
func UserFromState(id vo.UserID, email vo.Email, passwordHash, fullName, role string, status Status, version int64, createdAt, updatedAt time.Time) *User {
	return &User{
		id:           id,
		email:        email,
		passwordHash: passwordHash,
		fullName:     fullName,
		role:         role,
		status:       status,
		version:      version,
		createdAt:    createdAt,
		updatedAt:    updatedAt,
	}
}

// CanSignIn reports whether the account is allowed to authenticate.
func (u *User) CanSignIn() bool { return u.status == StatusActive }

// ChangePassword swaps the credential and records the security event.
func (u *User) ChangePassword(newHash string, now time.Time) {
	u.passwordHash = newHash
	u.updatedAt = now
	u.events = append(u.events, PasswordChanged{UserID: u.id.String(), OccurredAt: now})
}

// ChangeRole assigns a new role attribute and records the security event.
func (u *User) ChangeRole(role string, now time.Time) error {
	if role == "" || len(role) > 50 {
		return ErrInvalidRole
	}
	u.role = role
	u.updatedAt = now
	u.events = append(u.events, RoleChanged{UserID: u.id.String(), Role: role, OccurredAt: now})
	return nil
}

// PullEvents returns and clears recorded events after a successful save.
func (u *User) PullEvents() []Event {
	events := u.events
	u.events = nil
	return events
}

// ID returns the user id.
func (u *User) ID() vo.UserID { return u.id }

// Email returns the canonical address.
func (u *User) Email() vo.Email { return u.email }

// PasswordHash returns the stored credential hash.
func (u *User) PasswordHash() string { return u.passwordHash }

// FullName returns the display name.
func (u *User) FullName() string { return u.fullName }

// Role returns the authorization subject attribute.
func (u *User) Role() string { return u.role }

// Status returns the lifecycle state.
func (u *User) Status() Status { return u.status }

// Version returns the optimistic-locking version.
func (u *User) Version() int64 { return u.version }

// CreatedAt returns the registration time.
func (u *User) CreatedAt() time.Time { return u.createdAt }

// UpdatedAt returns the last-modified time.
func (u *User) UpdatedAt() time.Time { return u.updatedAt }
