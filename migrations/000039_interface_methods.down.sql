DROP INDEX IF EXISTS ix_action_types_implements_method_rid;
ALTER TABLE action_types DROP COLUMN IF EXISTS implements_method_rid;

DROP INDEX IF EXISTS ix_interface_methods_interface_rid;
DROP TABLE IF EXISTS interface_methods;
