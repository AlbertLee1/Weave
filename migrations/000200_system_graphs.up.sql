-- VTX-007 SystemGraph: persistent Vertex graph resources.
--
-- A system_graphs row stores the canonical Vertex graph payload (layers /
-- edges / saved selections / time settings / positions). When versioned=true,
-- every UPDATE writes a snapshot row to system_graph_versions via a trigger,
-- preserving full history. When versioned=false, updates happen in-place with
-- no history (ephemeral / per-user scratch graphs).

CREATE TABLE IF NOT EXISTS system_graphs (
    rid           TEXT PRIMARY KEY,
    ontology_rid  TEXT NOT NULL REFERENCES ontologies(rid) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    version       INT  NOT NULL DEFAULT 1,
    versioned     BOOLEAN NOT NULL DEFAULT TRUE,
    payload       JSONB NOT NULL DEFAULT '{"layers":[],"edges":[]}'::jsonb,
    created_by    TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'system_graphs_name_format') THEN
        ALTER TABLE system_graphs
            ADD CONSTRAINT system_graphs_name_format
            CHECK (length(name) BETWEEN 1 AND 256);
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS system_graphs_ontology_idx ON system_graphs(ontology_rid);

CREATE TABLE IF NOT EXISTS system_graph_versions (
    graph_rid  TEXT NOT NULL REFERENCES system_graphs(rid) ON DELETE CASCADE,
    version    INT  NOT NULL,
    payload    JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (graph_rid, version)
);

CREATE INDEX IF NOT EXISTS system_graph_versions_graph_idx
    ON system_graph_versions(graph_rid);

-- Auto-history trigger: on UPDATE to a row where versioned=true, snapshot the
-- NEW payload + version into system_graph_versions. Skip when versioned=false
-- so in-place layout/UI tweaks don't bloat history.
CREATE OR REPLACE FUNCTION system_graphs_write_history() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.versioned THEN
        INSERT INTO system_graph_versions (graph_rid, version, payload, created_at)
        VALUES (NEW.rid, NEW.version, NEW.payload, NEW.updated_at)
        ON CONFLICT (graph_rid, version) DO UPDATE
            SET payload = EXCLUDED.payload,
                created_at = EXCLUDED.created_at;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS system_graphs_write_history_trg ON system_graphs;
CREATE TRIGGER system_graphs_write_history_trg
    AFTER UPDATE ON system_graphs
    FOR EACH ROW
    EXECUTE FUNCTION system_graphs_write_history();
