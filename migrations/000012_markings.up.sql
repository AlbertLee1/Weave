-- Tier 3.3: Markings (Mandatory Access Control).
--
-- Palantir's signature security feature: objects can carry classification
-- labels (PUBLIC, INTERNAL, CONFIDENTIAL, PII, SECRET, ...). Users must hold
-- a corresponding "marking grant" to view an object that carries any given
-- marking. Unlike SecurityPolicies (ABAC allow/deny conditions), markings
-- are *mandatory* — there is no condition rule that can override a missing
-- grant; only an explicit grant in user_markings unlocks the marking.
--
-- The markings are stored on each indexed object document under the
-- reserved __markings keyword field. The OSS read path drops any row whose
-- __markings is not a subset of the requesting user's grants. If a row has
-- no __markings field at all, it is treated as PUBLIC and visible to
-- everyone (back-compat with existing un-marked datasets).

CREATE TABLE IF NOT EXISTS markings (
    name         TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description  TEXT,
    color        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_markings (
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    marking_name TEXT NOT NULL REFERENCES markings(name) ON DELETE CASCADE,
    granted_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by   TEXT,
    PRIMARY KEY (user_id, marking_name)
);

CREATE INDEX IF NOT EXISTS idx_user_markings_user ON user_markings (user_id);

-- Seed the standard 5-marking ladder. ON CONFLICT keeps re-runs idempotent
-- so this migration can be safely re-applied during local dev resets.
INSERT INTO markings (name, display_name, description, color) VALUES
    ('PUBLIC',       'Public',       'Unrestricted access',                 '#10b981'),
    ('INTERNAL',     'Internal',     'Internal use only',                   '#3b82f6'),
    ('CONFIDENTIAL', 'Confidential', 'Confidential information',            '#f59e0b'),
    ('PII',          'PII',          'Personally identifiable information', '#ef4444'),
    ('SECRET',       'Secret',       'Highly restricted access',            '#dc2626')
ON CONFLICT (name) DO NOTHING;
