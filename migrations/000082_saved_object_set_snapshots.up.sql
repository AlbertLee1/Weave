-- US-365: Immutable snapshot persistence for ObjectSets.
--
-- Extends both saved_object_sets (durable named definitions) and
-- object_set_snapshots (frozen membership rows from US-224) with three new
-- columns plus a shared monotonically-increasing snapshot_at sequence so
-- callers can assert byte-for-byte identical re-loads even after the
-- underlying data mutates:
--
--   definition_hash  TEXT         sha256 of the canonical JSON definition.
--                                 Lets the storage layer detect churn-free
--                                 re-saves and lets clients verify they got
--                                 the exact same definition back.
--   snapshot_at      BIGINT       Reference to the snapshot transaction at
--                                 which the row was frozen. Allocated from
--                                 saved_object_sets_snapshot_seq so every
--                                 write across both tables receives a globally
--                                 unique, monotonically-increasing id.
--   is_immutable     BOOLEAN      TRUE means this row is permanently retained
--                                 and the materialised PK list (when present)
--                                 must be served verbatim. FALSE means the
--                                 1h TTL reaper may delete it.
--
-- saved_object_sets additionally gains frozen_object_type / frozen_primary_keys
-- / frozen_truncated so a save-then-load round trip can return the exact PK
-- membership captured at save time, not the current live execution result.
-- These columns stay NULL for rows that were never frozen (legacy v1 rows
-- and any row whose caller chose not to materialise PKs).

CREATE SEQUENCE IF NOT EXISTS saved_object_sets_snapshot_seq;

ALTER TABLE saved_object_sets
    ADD COLUMN IF NOT EXISTS definition_hash      TEXT        NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS snapshot_at          BIGINT      NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS is_immutable         BOOLEAN     NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS frozen_object_type   TEXT,
    ADD COLUMN IF NOT EXISTS frozen_primary_keys  JSONB,
    ADD COLUMN IF NOT EXISTS frozen_truncated     BOOLEAN     NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_saved_object_sets_reaper
    ON saved_object_sets (is_immutable, created_at)
    WHERE is_immutable = FALSE;

CREATE INDEX IF NOT EXISTS idx_saved_object_sets_definition_hash
    ON saved_object_sets (ontology_api_name, definition_hash);

ALTER TABLE object_set_snapshots
    ADD COLUMN IF NOT EXISTS definition_hash TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS snapshot_at     BIGINT  NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS is_immutable    BOOLEAN NOT NULL DEFAULT TRUE;

CREATE INDEX IF NOT EXISTS idx_object_set_snapshots_definition_hash
    ON object_set_snapshots (ontology_api_name, definition_hash);
