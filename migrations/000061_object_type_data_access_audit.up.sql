-- US-264: per-ObjectType data-access audit toggle.
--
-- When audit_data_access is TRUE, the OSS read paths (GetObject, ListObjects,
-- SearchObjects, ListLinkedObjects, GetLinkedObject, loadObjectSet) emit an
-- audit_events row with action = 'data.access' for every successful read on
-- that ObjectType. Default FALSE preserves pre-US-264 behaviour (no
-- data-access audit) so existing ObjectTypes opt in explicitly via the admin
-- UI / PUT handler.

ALTER TABLE object_types
    ADD COLUMN IF NOT EXISTS audit_data_access BOOLEAN NOT NULL DEFAULT FALSE;
