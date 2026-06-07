-- Authorization module queries.

-- name: ListEnabledPolicies :many
SELECT id, name, effect, resource_type, action, condition, enabled, created_at, updated_at
FROM authz.policies
WHERE enabled = TRUE
ORDER BY name;

-- name: ListAllPolicies :many
SELECT id, name, effect, resource_type, action, condition, enabled, created_at, updated_at
FROM authz.policies
ORDER BY name;

-- name: UpsertPolicy :exec
INSERT INTO authz.policies (id, name, effect, resource_type, action, condition, enabled, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
ON CONFLICT (name) DO UPDATE
SET effect = EXCLUDED.effect,
    resource_type = EXCLUDED.resource_type,
    action = EXCLUDED.action,
    condition = EXCLUDED.condition,
    enabled = EXCLUDED.enabled,
    updated_at = EXCLUDED.updated_at;

-- name: DeletePolicy :execrows
DELETE FROM authz.policies WHERE name = $1;
