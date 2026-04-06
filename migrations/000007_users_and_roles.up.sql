-- RBAC Phase 1: users and roles tables.
-- See .omc/scientist/reports/20260406_104203_rbac_design.md for design rationale.
--
-- - users: identity rows (id is the canonical user id used everywhere in the app)
-- - user_roles: global role grants (admin / editor / viewer)
-- - user_ontology_roles: per-ontology scoped grants (currently only ontology-owner)

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT UNIQUE,
    password_hash TEXT,
    name          TEXT,
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE user_roles (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('viewer', 'editor', 'admin')),
    granted_at TIMESTAMPTZ DEFAULT now(),
    granted_by TEXT,
    PRIMARY KEY (user_id, role)
);

CREATE TABLE user_ontology_roles (
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ontology_rid TEXT NOT NULL REFERENCES ontologies(rid) ON DELETE CASCADE,
    role         TEXT NOT NULL CHECK (role = 'ontology-owner'),
    granted_at   TIMESTAMPTZ DEFAULT now(),
    granted_by   TEXT,
    PRIMARY KEY (user_id, ontology_rid, role)
);

CREATE INDEX idx_user_roles_user ON user_roles(user_id);
CREATE INDEX idx_user_ontology_roles_user ON user_ontology_roles(user_id);
CREATE INDEX idx_user_ontology_roles_ontology ON user_ontology_roles(ontology_rid);
