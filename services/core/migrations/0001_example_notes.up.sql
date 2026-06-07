-- Example module schema. Each module owns its own schema (here "example") so
-- it can be extracted into a standalone service mechanically later.
CREATE SCHEMA IF NOT EXISTS example;

CREATE TABLE example.notes (
    id          UUID PRIMARY KEY,
    owner_id    TEXT        NOT NULL,
    title       TEXT        NOT NULL,
    body        TEXT        NOT NULL DEFAULT '',
    -- version implements optimistic locking: UPDATE ... WHERE id = $1 AND
    -- version = $2; zero rows affected means a concurrent edit won.
    version     BIGINT      NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

-- Composite index matching the list query's WHERE + ORDER BY.
CREATE INDEX notes_owner_created_idx
    ON example.notes (owner_id, created_at DESC);

-- Transactional outbox: events are inserted in the SAME transaction as the
-- state change they describe, then relayed to the bus by the worker. This is
-- what removes the dual-write race.
CREATE TABLE example.outbox (
    id           UUID PRIMARY KEY,
    subject      TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ
);

-- The relay scans unpublished rows oldest-first.
CREATE INDEX outbox_unpublished_idx
    ON example.outbox (occurred_at)
    WHERE published_at IS NULL;
