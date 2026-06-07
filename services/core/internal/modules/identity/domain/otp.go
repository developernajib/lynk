package domain

import "time"

// OTPPurpose scopes a one-time code to exactly one flow, so a password-reset
// code can never verify an email.
type OTPPurpose string

const (
	// OTPPasswordReset codes are issued by RequestPasswordReset.
	OTPPasswordReset OTPPurpose = "password_reset"
	// OTPEmailVerify codes are issued by RequestEmailVerification.
	OTPEmailVerify OTPPurpose = "email_verify"
)

// OTP is one single-use, expiring code. Only the SHA-256 of the code lives
// here.
type OTP struct {
	id         string
	userID     string
	purpose    OTPPurpose
	codeHash   string
	expiresAt  time.Time
	consumedAt *time.Time
	createdAt  time.Time
}

// NewOTP records a freshly issued code.
func NewOTP(id, userID string, purpose OTPPurpose, codeHash string, expiresAt, now time.Time) *OTP {
	return &OTP{id: id, userID: userID, purpose: purpose, codeHash: codeHash, expiresAt: expiresAt, createdAt: now}
}

// OTPFromState rehydrates from storage.
func OTPFromState(id, userID string, purpose OTPPurpose, codeHash string, expiresAt time.Time, consumedAt *time.Time, createdAt time.Time) *OTP {
	return &OTP{id: id, userID: userID, purpose: purpose, codeHash: codeHash, expiresAt: expiresAt, consumedAt: consumedAt, createdAt: createdAt}
}

// IsActive reports whether the code may still be consumed.
func (o *OTP) IsActive(now time.Time) bool {
	return o.consumedAt == nil && now.Before(o.expiresAt)
}

// ID returns the code id.
func (o *OTP) ID() string { return o.id }

// UserID returns the owner.
func (o *OTP) UserID() string { return o.userID }

// Purpose returns the flow this code belongs to.
func (o *OTP) Purpose() OTPPurpose { return o.purpose }

// CodeHash returns the stored hash.
func (o *OTP) CodeHash() string { return o.codeHash }

// ExpiresAt returns the hard expiry.
func (o *OTP) ExpiresAt() time.Time { return o.expiresAt }

// CreatedAt returns the issue time.
func (o *OTP) CreatedAt() time.Time { return o.createdAt }
