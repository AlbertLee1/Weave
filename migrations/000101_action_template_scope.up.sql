-- US-427 Action 参数模板后端持久化与共享: extends US-320's two-state
-- (private | shared) visibility into a three-state scope ladder
-- (PRIVATE | TEAM | PUBLIC). The new column rides alongside the
-- legacy `shared` boolean so any caller still on the v1 wire shape
-- keeps working until the SPA + SDK ship the upgrade — write paths
-- below keep both columns in sync (PUBLIC ⇔ shared=TRUE; PRIVATE/TEAM
-- ⇔ shared=FALSE).
--
-- TEAM scope: "any user who shares at least one auth.Group with the
-- owner". The membership lookup is resolved at request time by the
-- handler against pkg/auth.GroupRepository — no foreign key here so a
-- group rename / membership change picks up immediately.

ALTER TABLE action_parameter_templates
    ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'PRIVATE';

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'action_parameter_templates_scope_enum') THEN
        ALTER TABLE action_parameter_templates
            ADD CONSTRAINT action_parameter_templates_scope_enum
            CHECK (scope IN ('PRIVATE','TEAM','PUBLIC'));
    END IF;
END$$;

-- Backfill: any pre-existing row that carried shared=TRUE under
-- US-320's two-state model becomes PUBLIC under the three-state one.
UPDATE action_parameter_templates SET scope = 'PUBLIC'
    WHERE shared = TRUE AND scope = 'PRIVATE';

CREATE INDEX IF NOT EXISTS action_parameter_templates_scope_lookup_idx
    ON action_parameter_templates(ontology, action_type, scope);
