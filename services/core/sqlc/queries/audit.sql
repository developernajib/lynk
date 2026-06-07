-- Audit module queries. The ledger is append-only: insert and read, nothing
-- else.

-- name: InsertAuditEntry :exec
INSERT INTO audit.entries (id, subject, payload, occurred_at, recorded_at)
VALUES ($1, $2, $3, $4, $5);

-- name: ListAuditEntries :many
SELECT id, subject, payload, occurred_at, recorded_at
FROM audit.entries
WHERE (sqlc.arg(subject_prefix)::text = '' OR subject LIKE sqlc.arg(subject_prefix) || '%')
ORDER BY recorded_at DESC
LIMIT $1 OFFSET $2;
