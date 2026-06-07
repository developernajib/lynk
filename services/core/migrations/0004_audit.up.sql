-- Audit module: an append-only ledger written by a durable consumer over
-- every core event subject. No UPDATE or DELETE path exists by design.
CREATE SCHEMA IF NOT EXISTS audit;

CREATE TABLE audit.entries (
    id          UUID PRIMARY KEY,
    subject     TEXT        NOT NULL,
    payload     JSONB       NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);

-- The list query orders newest-first and filters by subject prefix.
CREATE INDEX audit_entries_recorded_idx ON audit.entries (recorded_at DESC);
CREATE INDEX audit_entries_subject_idx ON audit.entries (subject text_pattern_ops);
