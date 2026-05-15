-- VTX-009 / VTX-012 SystemGraph Templates.
--
-- A graph template is a snapshot of an existing graph plus a list of JSON
-- pointer-style paths that VTX-012's Instantiate will substitute at
-- run-time. This story only writes the table and the basic Create/Get; the
-- parameterized instantiate flow lands in VTX-012.

CREATE TABLE IF NOT EXISTS graph_templates (
    rid                   TEXT PRIMARY KEY,
    source_graph_rid      TEXT NOT NULL REFERENCES system_graphs(rid) ON DELETE CASCADE,
    name                  TEXT NOT NULL,
    payload               JSONB NOT NULL,
    parameterized_fields  JSONB NOT NULL DEFAULT '[]'::jsonb,
    parameters            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by            TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'graph_templates_name_format') THEN
        ALTER TABLE graph_templates
            ADD CONSTRAINT graph_templates_name_format
            CHECK (length(name) BETWEEN 1 AND 256);
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS graph_templates_source_idx
    ON graph_templates(source_graph_rid);
