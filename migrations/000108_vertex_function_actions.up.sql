-- VTX-051 Function-backed Action 注册: per-Vertex binding rows that pair
-- an OMS function-backed ActionType (action_types.is_function_backed=true
-- + action_types.function_rid pointing at oms.functions) with the
-- Vertex-specific OutputMapping rules that describe how the bound
-- function's flat output payload becomes property edits inside a
-- Scenario fork (scenario_edits).
--
-- The action_types table already carries function_rid + is_function_backed
-- columns (since 000001_initial_schema). This migration only adds the
-- Vertex-side binding row — the OMS layer stays oblivious to the
-- Vertex-specific output mapping shape, and a single function-backed
-- ActionType can carry a per-(ontology, action_type) row without
-- mutating OMS rows the rest of Weave already relies on.

CREATE TABLE IF NOT EXISTS vertex_function_actions (
    rid              TEXT PRIMARY KEY,
    ontology_rid     TEXT NOT NULL REFERENCES ontologies(rid) ON DELETE CASCADE,
    action_type_rid  TEXT NOT NULL,
    function_rid     TEXT NOT NULL,
    output_mappings  JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(ontology_rid, action_type_rid)
);

CREATE INDEX IF NOT EXISTS vertex_function_actions_ontology_idx
    ON vertex_function_actions(ontology_rid);

CREATE INDEX IF NOT EXISTS vertex_function_actions_action_type_idx
    ON vertex_function_actions(action_type_rid);

CREATE INDEX IF NOT EXISTS vertex_function_actions_function_idx
    ON vertex_function_actions(function_rid);
