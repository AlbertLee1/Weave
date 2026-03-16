-- Weave Ontology Layer: Initial Schema
-- 11 core tables for Ontology Metadata Service

CREATE TABLE ontologies (
    rid          TEXT PRIMARY KEY,
    api_name     TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL,
    description  TEXT,
    created_at   TIMESTAMPTZ DEFAULT now(),
    updated_at   TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE object_types (
    rid                 TEXT PRIMARY KEY,
    ontology_rid        TEXT NOT NULL REFERENCES ontologies(rid),
    api_name            TEXT NOT NULL,
    display_name        TEXT NOT NULL,
    plural_display_name TEXT,
    description         TEXT,
    primary_key_prop    TEXT NOT NULL,
    title_property      TEXT,
    status              TEXT DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'EXPERIMENTAL', 'DEPRECATED')),
    visibility          TEXT DEFAULT 'NORMAL' CHECK (visibility IN ('PROMINENT', 'NORMAL', 'HIDDEN')),
    icon_name           TEXT,
    color               TEXT,
    created_at          TIMESTAMPTZ DEFAULT now(),
    updated_at          TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, api_name)
);

CREATE TABLE properties (
    rid             TEXT PRIMARY KEY,
    object_type_rid TEXT NOT NULL REFERENCES object_types(rid) ON DELETE CASCADE,
    api_name        TEXT NOT NULL,
    display_name    TEXT,
    description     TEXT,
    base_type       TEXT NOT NULL,
    type_config     JSONB DEFAULT '{}',
    is_array        BOOLEAN DEFAULT false,
    is_nullable     BOOLEAN DEFAULT true,
    is_searchable   BOOLEAN DEFAULT true,
    is_sortable     BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(object_type_rid, api_name)
);

CREATE TABLE link_types (
    rid                TEXT PRIMARY KEY,
    ontology_rid       TEXT NOT NULL REFERENCES ontologies(rid),
    api_name           TEXT NOT NULL,
    display_name       TEXT NOT NULL,
    description        TEXT,
    source_object_type TEXT NOT NULL REFERENCES object_types(rid),
    target_object_type TEXT NOT NULL REFERENCES object_types(rid),
    cardinality        TEXT NOT NULL CHECK (cardinality IN ('ONE_TO_ONE', 'ONE_TO_MANY', 'MANY_TO_MANY')),
    foreign_key_config JSONB,
    join_table_config  JSONB,
    is_required        BOOLEAN DEFAULT false,
    created_at         TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, api_name)
);

CREATE TABLE action_types (
    rid                TEXT PRIMARY KEY,
    ontology_rid       TEXT NOT NULL REFERENCES ontologies(rid),
    api_name           TEXT NOT NULL,
    display_name       TEXT NOT NULL,
    description        TEXT,
    status             TEXT DEFAULT 'ACTIVE',
    parameters         JSONB NOT NULL DEFAULT '[]',
    rules              JSONB NOT NULL DEFAULT '[]',
    submission_criteria JSONB DEFAULT '{}',
    side_effects       JSONB DEFAULT '[]',
    function_rid       TEXT,
    is_function_backed BOOLEAN DEFAULT false,
    created_at         TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, api_name)
);

CREATE TABLE interfaces (
    rid               TEXT PRIMARY KEY,
    ontology_rid      TEXT NOT NULL REFERENCES ontologies(rid),
    api_name          TEXT NOT NULL,
    display_name      TEXT NOT NULL,
    extends_rid       TEXT REFERENCES interfaces(rid),
    shared_properties JSONB DEFAULT '[]',
    created_at        TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, api_name)
);

CREATE TABLE object_type_interfaces (
    object_type_rid  TEXT NOT NULL REFERENCES object_types(rid) ON DELETE CASCADE,
    interface_rid    TEXT NOT NULL REFERENCES interfaces(rid) ON DELETE CASCADE,
    property_mapping JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (object_type_rid, interface_rid)
);

CREATE TABLE value_types (
    rid          TEXT PRIMARY KEY,
    api_name     TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    base_type    TEXT NOT NULL,
    constraints  JSONB DEFAULT '{}',
    version      INTEGER DEFAULT 1,
    created_at   TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE datasource_bindings (
    rid             TEXT PRIMARY KEY,
    object_type_rid TEXT NOT NULL REFERENCES object_types(rid),
    dataset_rid     TEXT NOT NULL,
    branch          TEXT DEFAULT 'main',
    column_mapping  JSONB NOT NULL,
    is_primary      BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE security_policies (
    rid             TEXT PRIMARY KEY,
    object_type_rid TEXT NOT NULL REFERENCES object_types(rid),
    policy_type     TEXT NOT NULL CHECK (policy_type IN ('OBJECT', 'PROPERTY')),
    rules           JSONB NOT NULL,
    created_at      TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE action_logs (
    id              BIGSERIAL PRIMARY KEY,
    action_type_rid TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    parameters      JSONB NOT NULL,
    edits           JSONB NOT NULL,
    status          TEXT NOT NULL,
    error_message   TEXT,
    created_at      TIMESTAMPTZ DEFAULT now()
);

-- Indexes
CREATE INDEX idx_properties_object_type ON properties(object_type_rid);
CREATE INDEX idx_link_types_source ON link_types(source_object_type);
CREATE INDEX idx_link_types_target ON link_types(target_object_type);
CREATE INDEX idx_action_logs_type ON action_logs(action_type_rid);
CREATE INDEX idx_action_logs_created ON action_logs(created_at);
CREATE INDEX idx_datasource_bindings_obj ON datasource_bindings(object_type_rid);
