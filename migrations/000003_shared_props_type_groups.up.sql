-- Shared Properties
CREATE TABLE IF NOT EXISTS shared_properties (
    rid           TEXT PRIMARY KEY,
    ontology_rid  TEXT NOT NULL REFERENCES ontologies(rid) ON DELETE CASCADE,
    api_name      TEXT NOT NULL,
    display_name  TEXT,
    description   TEXT,
    base_type     TEXT NOT NULL,
    type_config   JSONB DEFAULT '{}',
    is_array      BOOLEAN DEFAULT false,
    created_at    TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, api_name)
);

-- Type Groups
CREATE TABLE IF NOT EXISTS type_groups (
    rid           TEXT PRIMARY KEY,
    ontology_rid  TEXT NOT NULL REFERENCES ontologies(rid) ON DELETE CASCADE,
    api_name      TEXT NOT NULL,
    display_name  TEXT NOT NULL,
    description   TEXT,
    color         TEXT,
    created_at    TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, api_name)
);

-- Object Type <-> Type Group many-to-many
CREATE TABLE IF NOT EXISTS object_type_groups (
    object_type_rid TEXT NOT NULL REFERENCES object_types(rid) ON DELETE CASCADE,
    type_group_rid  TEXT NOT NULL REFERENCES type_groups(rid) ON DELETE CASCADE,
    PRIMARY KEY (object_type_rid, type_group_rid)
);

-- Link properties to shared properties
ALTER TABLE properties ADD COLUMN IF NOT EXISTS shared_property_rid TEXT REFERENCES shared_properties(rid);
