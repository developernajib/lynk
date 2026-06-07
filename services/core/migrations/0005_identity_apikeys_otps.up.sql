-- API keys and one-time codes for the identity module, plus email
-- verification state on users.
ALTER TABLE identity.users ADD COLUMN email_verified_at TIMESTAMPTZ;

CREATE TABLE identity.api_keys (
    id         UUID PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    -- Only the SHA-256 of the key is stored; the secret is shown once.
    key_hash   TEXT        NOT NULL UNIQUE,
    -- prefix is a displayable hint ("lynk_AB12"), never enough to use.
    prefix     TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX api_keys_user_idx ON identity.api_keys (user_id);

CREATE TABLE identity.otps (
    id          UUID PRIMARY KEY,
    user_id     UUID        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    -- purpose scopes a code to one flow: 'password_reset' or 'email_verify'.
    purpose     TEXT        NOT NULL,
    -- Only the SHA-256 of the code is stored.
    code_hash   TEXT        NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL
);

-- The lookup is always "newest live code for this user and purpose".
CREATE INDEX otps_user_purpose_idx ON identity.otps (user_id, purpose, created_at DESC);
