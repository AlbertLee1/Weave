-- US-251: Users/Groups/Roles model.
--
-- Adds four new tables that bring dynamic RBAC to the platform:
--
--   groups           named collections of users (e.g. "analysts-na", "ingest-bots")
--   user_groups      membership relation between users and groups
--   roles            role registry — built-in (viewer/editor/admin/...) and custom
--   role_permissions permission grants per role — used for dynamic roles beyond the
--                    static matrix in pkg/auth/permissions.go
--
-- The built-in roles are seeded so the dynamic `roles` table is consistent with
-- the static matrix from day one. `role_permissions` rows for built-ins are NOT
-- seeded here — the authoritative source remains the code-level matrix; this
-- column exists for custom roles and future migration of the resolver to a
-- DB-backed lookup.
--
-- The existing `user_roles` table keeps its CHECK constraint (viewer/editor/admin)
-- untouched. A future story migrating dynamic role grants to `user_roles` can
-- drop that CHECK and replace it with an FK; US-251 is scope-limited to the
-- new model + CRUD API, not the resolver rewrite.

CREATE TABLE groups (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_groups_name ON groups (name);

CREATE TABLE user_groups (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, group_id)
);

CREATE INDEX idx_user_groups_group ON user_groups(group_id);

CREATE TABLE roles (
    name        TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    builtin     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE role_permissions (
    role_name  TEXT NOT NULL REFERENCES roles(name) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    PRIMARY KEY (role_name, permission)
);

CREATE INDEX idx_role_permissions_role ON role_permissions(role_name);

-- Seed built-in role identities so the roles table is consistent with the
-- static matrix in pkg/auth/permissions.go. Built-ins cannot be deleted via
-- the admin API (enforced at the handler layer).
INSERT INTO roles (name, description, builtin) VALUES
    ('viewer',         'Read-only access to ontology metadata and objects', TRUE),
    ('editor',         'Read + write objects and execute actions',          TRUE),
    ('ontology-owner', 'Full write access within one ontology',             TRUE),
    ('admin',          'Full administrative access across the platform',    TRUE),
    ('ingest-writer',  'Stream ingest endpoint only',                       TRUE)
ON CONFLICT (name) DO NOTHING;
