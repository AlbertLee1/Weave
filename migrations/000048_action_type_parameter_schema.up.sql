-- US-245: Parameter Validation DSL.
-- Adds a JSON Schema (Draft-07) document per ActionType that the Prepare flow
-- evaluates after the legacy type/required validator. Optional — legacy
-- ActionTypes without a schema continue to rely on ParameterDef alone. Stored
-- as JSONB so PG can round-trip the Draft-07 structure without string parsing.

ALTER TABLE action_types
    ADD COLUMN IF NOT EXISTS parameter_schema JSONB;
