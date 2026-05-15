-- VTX-001 Vertex Scenarios: Case Studies + Scenarios + per-scenario edit delta
-- (objects/links) + Model Mesh parameter overrides.
--
-- Vertex models Scenario as an application-layer fork: a Scenario carries a
-- list of edits over a base ontology commit. Read APIs that pass an
-- X-Scenario-Id header fold these edits onto the base view; writes go to
-- scenario_edits, never to the underlying object tables. Freezing a Scenario
-- sets immutable=true; further appends are rejected at the application layer
-- (VTX-002 ScenarioRepo).

CREATE TABLE IF NOT EXISTS case_studies (
    rid          TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    ontology_rid TEXT NOT NULL REFERENCES ontologies(rid) ON DELETE CASCADE,
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'case_studies_name_format') THEN
        ALTER TABLE case_studies
            ADD CONSTRAINT case_studies_name_format
            CHECK (length(name) BETWEEN 1 AND 256);
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS case_studies_ontology_idx ON case_studies(ontology_rid);

CREATE TABLE IF NOT EXISTS scenarios (
    rid                    TEXT PRIMARY KEY,
    case_study_rid         TEXT NOT NULL REFERENCES case_studies(rid) ON DELETE CASCADE,
    name                   TEXT NOT NULL,
    parent_ontology_commit TEXT NOT NULL DEFAULT '',
    status                 TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'frozen', 'archived')),
    immutable              BOOLEAN NOT NULL DEFAULT FALSE,
    created_by             TEXT NOT NULL DEFAULT '',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'scenarios_name_format') THEN
        ALTER TABLE scenarios
            ADD CONSTRAINT scenarios_name_format
            CHECK (length(name) BETWEEN 1 AND 256);
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS scenarios_case_study_idx ON scenarios(case_study_rid);

CREATE TABLE IF NOT EXISTS scenario_edits (
    scenario_rid TEXT NOT NULL REFERENCES scenarios(rid) ON DELETE CASCADE,
    seq          BIGSERIAL,
    op           TEXT NOT NULL
        CHECK (op IN ('createObject', 'modifyProperty', 'deleteObject', 'addLink', 'deleteLink')),
    object_type  TEXT,
    object_id    TEXT,
    property     TEXT,
    new_value    JSONB,
    link_type    TEXT,
    src_id       TEXT,
    dst_id       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scenario_rid, seq)
);

CREATE INDEX IF NOT EXISTS scenario_edits_scenario_object_idx
    ON scenario_edits(scenario_rid, object_id);

CREATE TABLE IF NOT EXISTS scenario_overrides (
    scenario_rid TEXT NOT NULL REFERENCES scenarios(rid) ON DELETE CASCADE,
    model_rid    TEXT NOT NULL,
    parameter    TEXT NOT NULL,
    object_id    TEXT NOT NULL DEFAULT '',
    value        JSONB NOT NULL,
    applied_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scenario_rid, model_rid, parameter, object_id)
);
