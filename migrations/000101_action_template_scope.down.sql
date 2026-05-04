DROP INDEX IF EXISTS action_parameter_templates_scope_lookup_idx;

ALTER TABLE action_parameter_templates DROP CONSTRAINT IF EXISTS action_parameter_templates_scope_enum;

ALTER TABLE action_parameter_templates DROP COLUMN IF EXISTS scope;
