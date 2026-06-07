-- Authorization module: ABAC policies as data. The condition column holds a
-- CEL expression compiled and cached in-process by the checker.
CREATE SCHEMA IF NOT EXISTS authz;

CREATE TABLE authz.policies (
    id            UUID PRIMARY KEY,
    name          TEXT        NOT NULL UNIQUE,
    -- 'allow' or 'deny'; any matching deny overrides every allow.
    effect        TEXT        NOT NULL CHECK (effect IN ('allow', 'deny')),
    -- '*' matches every resource type / action.
    resource_type TEXT        NOT NULL,
    action        TEXT        NOT NULL,
    condition     TEXT        NOT NULL,
    enabled       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL
);

-- Seed defaults: admins can do everything; owners can act on their own
-- resources (modules pass owner_id in the resource attributes). UUIDv7-style
-- fixed ids keep the seed idempotent and recognizable.
INSERT INTO authz.policies (id, name, effect, resource_type, action, condition, enabled, created_at, updated_at) VALUES
  ('01970000-0000-7000-8000-00000000a001', 'admin-full-access', 'allow', '*', '*',
   'subject["role"] == "admin"', TRUE, NOW(), NOW()),
  ('01970000-0000-7000-8000-00000000a002', 'resource-owner-access', 'allow', '*', '*',
   'resource["owner_id"] == subject["id"]', TRUE, NOW(), NOW());
