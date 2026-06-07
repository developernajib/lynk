-- Identity module schema: users, refresh sessions, and the module's own
-- outbox (each module owns its outbox so it can be extracted mechanically).
CREATE SCHEMA IF NOT EXISTS identity;

CREATE TABLE identity.users (
    id            UUID PRIMARY KEY,
    email         TEXT        NOT NULL UNIQUE,
    -- password_hash is argon2id in PHC format (bcrypt verifies for imports).
    password_hash TEXT        NOT NULL,
    full_name     TEXT        NOT NULL,
    role          TEXT        NOT NULL DEFAULT 'user',
    status        TEXT        NOT NULL DEFAULT 'active',
    version       BIGINT      NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL
);

CREATE TABLE identity.refresh_tokens (
    id          UUID PRIMARY KEY,
    user_id     UUID        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    -- Only the SHA-256 of the opaque token is stored; a database leak
    -- exposes nothing replayable.
    token_hash  TEXT        NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL
);

-- Revoke-all-sessions and cleanup both scan by user.
CREATE INDEX refresh_tokens_user_idx ON identity.refresh_tokens (user_id);

CREATE TABLE identity.outbox (
    id           UUID PRIMARY KEY,
    subject      TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ
);

CREATE INDEX identity_outbox_unpublished_idx
    ON identity.outbox (occurred_at)
    WHERE published_at IS NULL;
