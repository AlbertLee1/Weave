-- VTX-050 Live Model Deployment Wrapper: persisted metadata for an
-- external HTTP-served model alongside the auto-generated wrapper
-- oms.Function row (via pkg/vertex/modelfunctions.BuildWrapperFunction).
--
-- A row in model_deployments is the authoritative description of a
-- live model: its HTTP endpoint, the I/O schema the wrapper exposes,
-- and the function_rid pointing at the oms.Function row Vertex
-- Scenarios actually reference. Keeping the two tables linked rather
-- than collapsing into oms.functions keeps the deployment-side fields
-- (endpoint, model_version) out of the Function row's signature
-- JSONB — which downstream Action wiring (VTX-051) treats as the
-- canonical I/O contract.

CREATE TABLE IF NOT EXISTS model_deployments (
    rid            TEXT PRIMARY KEY,
    ontology_rid   TEXT NOT NULL REFERENCES ontologies(rid) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    endpoint_url   TEXT NOT NULL,
    model_version  TEXT NOT NULL DEFAULT '',
    inputs         JSONB NOT NULL DEFAULT '[]'::jsonb,
    output         JSONB NOT NULL DEFAULT 'null'::jsonb,
    function_rid   TEXT NOT NULL DEFAULT '',
    created_by     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'model_deployments_name_format') THEN
        ALTER TABLE model_deployments
            ADD CONSTRAINT model_deployments_name_format
            CHECK (length(name) BETWEEN 1 AND 256);
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'model_deployments_endpoint_required') THEN
        ALTER TABLE model_deployments
            ADD CONSTRAINT model_deployments_endpoint_required
            CHECK (length(endpoint_url) > 0);
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS model_deployments_ontology_idx
    ON model_deployments(ontology_rid);

CREATE INDEX IF NOT EXISTS model_deployments_function_idx
    ON model_deployments(function_rid)
    WHERE function_rid <> '';
