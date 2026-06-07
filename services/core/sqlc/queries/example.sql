-- Example module queries. sqlc compiles these into type-safe Go under
-- internal/gen/db; repositories call the generated methods, never raw SQL.

-- name: CreateNote :exec
INSERT INTO example.notes (id, owner_id, title, body, version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- Reads are owner-scoped so one user can never address another's note by id.
-- name: GetNote :one
SELECT id, owner_id, title, body, version, created_at, updated_at
FROM example.notes
WHERE id = $1 AND owner_id = $2;

-- UpdateNote is version-guarded: :execrows returns the affected-row count so
-- the repository can turn zero rows into a concurrency conflict.
-- name: UpdateNote :execrows
UPDATE example.notes
SET title = $3, body = $4, version = version + 1, updated_at = $5
WHERE id = $1 AND owner_id = $2 AND version = $6;

-- name: ListNotes :many
SELECT id, owner_id, title, body, version, created_at, updated_at
FROM example.notes
WHERE owner_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: InsertOutboxEvent :exec
INSERT INTO example.outbox (id, subject, payload, occurred_at)
VALUES ($1, $2, $3, $4);

-- The relay claims a batch with FOR UPDATE SKIP LOCKED so multiple worker
-- replicas never double-publish the same row.
-- name: ClaimUnpublishedOutboxEvents :many
SELECT id, subject, payload, occurred_at
FROM example.outbox
WHERE published_at IS NULL
ORDER BY occurred_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxEventPublished :exec
UPDATE example.outbox
SET published_at = $2
WHERE id = $1;
