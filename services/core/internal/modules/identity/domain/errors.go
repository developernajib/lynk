// Package domain is the identity module's core: the User and RefreshToken
// aggregates, value objects, events, errors, and repository ports. Stdlib
// only; adapters translate these sentinels at the boundary.
package domain

import "errors"

// ErrUserNotFound means no user matches the identifier.
var ErrUserNotFound = errors.New("user not found")

// ErrEmailTaken means the unique email constraint fired on registration.
var ErrEmailTaken = errors.New("email already registered")

// ErrInvalidCredentials covers both unknown email and wrong password: one
// sentinel so responses cannot be used to enumerate accounts.
var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrAccountLocked means too many failed logins; try again later.
var ErrAccountLocked = errors.New("too many failed login attempts")

// ErrAccountDisabled means the account exists but may not sign in.
var ErrAccountDisabled = errors.New("account disabled")

// ErrRefreshTokenInvalid covers unknown, expired, and revoked refresh
// tokens: one sentinel, same enumeration reasoning as credentials.
var ErrRefreshTokenInvalid = errors.New("refresh token invalid")

// ErrConcurrentUpdate means another writer won the optimistic-lock race.
var ErrConcurrentUpdate = errors.New("user was modified concurrently")

// ErrInvalidOTP covers unknown, expired, consumed, and mismatched codes:
// one sentinel, same enumeration reasoning as credentials.
var ErrInvalidOTP = errors.New("invalid or expired code")

// ErrAPIKeyNotFound means no live key matches; covers revoked keys too.
var ErrAPIKeyNotFound = errors.New("api key not found")
