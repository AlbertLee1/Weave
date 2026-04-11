-- US-019: Tag object_history rows with the writer source so US-021 edit
-- conflict resolution can prefer user edits over concurrent ingest edits.
-- Existing rows are back-filled with 'user' because the action executor was
-- the only writer before Phase 6 and every historical row is therefore a
-- user edit.

ALTER TABLE object_history
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'user';

CREATE INDEX IF NOT EXISTS idx_object_history_source
    ON object_history (object_type_rid, primary_key, source, version DESC);
