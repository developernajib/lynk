-- Identity module queries.

-- name: CreateUser :exec
INSERT INTO identity.users (id, email, password_hash, full_name, role, status, version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetUserByEmail :one
SELECT id, email, password_hash, full_name, role, status, version, created_at, updated_at, email_verified_at
FROM identity.users
WHERE email = $1;

-- name: GetUserByID :one
SELECT id, email, password_hash, full_name, role, status, version, created_at, updated_at, email_verified_at
FROM identity.users
WHERE id = $1;

-- Version-guarded update: zero affected rows = concurrent change.
-- name: UpdateUser :execrows
UPDATE identity.users
SET password_hash = $2, full_name = $3, role = $4, status = $5,
    version = version + 1, updated_at = $6
WHERE id = $1 AND version = $7;

-- name: CreateRefreshToken :exec
INSERT INTO identity.refresh_tokens (id, user_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5);

-- name: GetRefreshTokenByHash :one
SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
FROM identity.refresh_tokens
WHERE token_hash = $1;

-- name: RevokeRefreshToken :exec
UPDATE identity.refresh_tokens
SET revoked_at = $2
WHERE id = $1 AND revoked_at IS NULL;

-- ChangePassword and security events revoke every live session at once.
-- name: RevokeAllRefreshTokensForUser :exec
UPDATE identity.refresh_tokens
SET revoked_at = $2
WHERE user_id = $1 AND revoked_at IS NULL;

-- Used by the seeder for idempotent admin promotion.
-- name: SetUserRoleByEmail :execrows
UPDATE identity.users
SET role = $2, version = version + 1, updated_at = $3
WHERE email = $1;

-- name: SetEmailVerified :exec
UPDATE identity.users
SET email_verified_at = $2, version = version + 1, updated_at = $2
WHERE id = $1 AND email_verified_at IS NULL;

-- name: CreateAPIKey :exec
INSERT INTO identity.api_keys (id, user_id, name, key_hash, prefix, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListAPIKeysForUser :many
SELECT id, user_id, name, key_hash, prefix, created_at, revoked_at
FROM identity.api_keys
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetAPIKeyByHash :one
SELECT id, user_id, name, key_hash, prefix, created_at, revoked_at
FROM identity.api_keys
WHERE key_hash = $1;

-- Owner-scoped so one user can never revoke another's key by id.
-- name: RevokeAPIKey :execrows
UPDATE identity.api_keys
SET revoked_at = $3
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: CreateOTP :exec
INSERT INTO identity.otps (id, user_id, purpose, code_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- The newest live code wins; issuing a new code supersedes older ones.
-- name: GetActiveOTP :one
SELECT id, user_id, purpose, code_hash, expires_at, consumed_at, created_at
FROM identity.otps
WHERE user_id = $1 AND purpose = $2 AND consumed_at IS NULL AND expires_at > $3
ORDER BY created_at DESC
LIMIT 1;

-- name: ConsumeOTP :execrows
UPDATE identity.otps
SET consumed_at = $2
WHERE id = $1 AND consumed_at IS NULL;

-- name: InsertIdentityOutboxEvent :exec
INSERT INTO identity.outbox (id, subject, payload, occurred_at)
VALUES ($1, $2, $3, $4);

-- name: ClaimUnpublishedIdentityOutboxEvents :many
SELECT id, subject, payload, occurred_at
FROM identity.outbox
WHERE published_at IS NULL
ORDER BY occurred_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: MarkIdentityOutboxEventPublished :exec
UPDATE identity.outbox
SET published_at = $2
WHERE id = $1;
