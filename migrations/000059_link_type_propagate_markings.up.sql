-- US-261: marking inheritance via LinkType propagation.
--
-- When a LinkType declares propagate_markings = TRUE, every subsequent link
-- creation copies the source object's markings into the target object's
-- marking set so child objects inherit parent classifications without an
-- admin having to grant them per-row.
--
-- Default FALSE preserves pre-US-261 behaviour: existing link types keep
-- creating links without touching markings. Admins opt in per LinkType via
-- the admin handlers (POST/PUT /api/admin/linkTypes).

ALTER TABLE link_types
    ADD COLUMN IF NOT EXISTS propagate_markings BOOLEAN NOT NULL DEFAULT FALSE;
