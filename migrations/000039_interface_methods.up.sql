-- US-214: Interface Method Signatures. Interfaces now carry named methods
-- (params + returns) that ActionTypes may claim to implement via the new
-- `implements_method_rid` pointer on action_types. This is the metadata that
-- powers polymorphic dispatch: invoking an interface method on an ObjectType
-- finds the ActionType whose `implements_method_rid` matches and whose rule
-- targets that ObjectType's apiName.
--
-- The table mirrors the shape of other narrow admin tables (link_properties,
-- computed_properties): explicit FK to interfaces with ON DELETE CASCADE, and
-- a UNIQUE (interface_rid, name) so a method name is unique within its
-- owning Interface (a single method may be re-declared across sibling
-- Interfaces, which is intentional — each Interface owns its own method
-- namespace).
--
-- `params` is JSONB ordered array of {name, type, required, default}, and
-- `returns` is a JSONB object of {type}. The validator at write time checks
-- only that the JSON is an array / object respectively; deeper runtime
-- validation of actual call args is delegated to the ActionType's own
-- parameter validator when the invocation path routes the call through.
--
-- `implements_method_rid` on action_types is NULLable text, same pattern
-- as link_types.inverse_link_rid and object_types.extends_rid — NO FK
-- REFERENCES (so an ActionType authored before its method lands does not
-- error at insert time; the admin handler validates existence at write).

CREATE TABLE IF NOT EXISTS interface_methods (
    rid           TEXT PRIMARY KEY,
    interface_rid TEXT NOT NULL REFERENCES interfaces(rid) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    params        JSONB NOT NULL DEFAULT '[]'::jsonb,
    returns       JSONB NOT NULL DEFAULT '{}'::jsonb,
    description   TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (interface_rid, name)
);

CREATE INDEX IF NOT EXISTS ix_interface_methods_interface_rid
    ON interface_methods(interface_rid);

ALTER TABLE action_types
    ADD COLUMN IF NOT EXISTS implements_method_rid TEXT;

CREATE INDEX IF NOT EXISTS ix_action_types_implements_method_rid
    ON action_types(implements_method_rid)
    WHERE implements_method_rid IS NOT NULL;
