package domain

import "time"

// APIKey is a machine credential. Only the SHA-256 of the secret lives here;
// the raw key exists once, in the create response.
type APIKey struct {
	id        string
	userID    string
	name      string
	keyHash   string
	prefix    string
	createdAt time.Time
	revokedAt *time.Time
}

// NewAPIKey records a freshly minted key.
func NewAPIKey(id, userID, name, keyHash, prefix string, now time.Time) *APIKey {
	return &APIKey{id: id, userID: userID, name: name, keyHash: keyHash, prefix: prefix, createdAt: now}
}

// APIKeyFromState rehydrates from storage.
func APIKeyFromState(id, userID, name, keyHash, prefix string, createdAt time.Time, revokedAt *time.Time) *APIKey {
	return &APIKey{id: id, userID: userID, name: name, keyHash: keyHash, prefix: prefix, createdAt: createdAt, revokedAt: revokedAt}
}

// IsActive reports whether the key may authenticate.
func (k *APIKey) IsActive() bool { return k.revokedAt == nil }

// ID returns the key id.
func (k *APIKey) ID() string { return k.id }

// UserID returns the owner.
func (k *APIKey) UserID() string { return k.userID }

// Name returns the caller-chosen label.
func (k *APIKey) Name() string { return k.name }

// KeyHash returns the stored hash.
func (k *APIKey) KeyHash() string { return k.keyHash }

// Prefix returns the displayable hint.
func (k *APIKey) Prefix() string { return k.prefix }

// CreatedAt returns the mint time.
func (k *APIKey) CreatedAt() time.Time { return k.createdAt }

// Revoked reports whether the key was disabled.
func (k *APIKey) Revoked() bool { return k.revokedAt != nil }
