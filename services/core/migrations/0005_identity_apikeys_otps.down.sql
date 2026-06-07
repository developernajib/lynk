DROP TABLE IF EXISTS identity.otps;
DROP TABLE IF EXISTS identity.api_keys;
ALTER TABLE identity.users DROP COLUMN IF EXISTS email_verified_at;
