-- US-203: Media Properties storage layer. Introduces a content-addressed
-- media_assets catalog decoupled from the legacy attachment store. Each row
-- references a deduplicated blob on local disk at
--   data/media/{realm}/{yyyy}/{mm}/{sha256}
-- Multiple rows MAY point at the same {realm, sha256} pair — the file is
-- stored once and the table acts as the reference counter. Deleting the last
-- row referencing a sha256 is when the physical file can be reclaimed.
--
-- Columns mirror the PRD: rid (primary key), mime, size, sha256, path,
-- created_by. `realm` is captured separately so the FS layout is derivable
-- from the row alone and the (realm, sha256) uniqueness can be enforced by
-- the storage layer.

CREATE TABLE IF NOT EXISTS media_assets (
    rid          TEXT PRIMARY KEY,
    realm        TEXT NOT NULL DEFAULT 'main',
    filename     TEXT NOT NULL DEFAULT '',
    mime         TEXT NOT NULL DEFAULT 'application/octet-stream',
    size_bytes   BIGINT NOT NULL,
    sha256       TEXT NOT NULL,
    path         TEXT NOT NULL,
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS media_assets_sha256_idx
    ON media_assets(realm, sha256);

CREATE INDEX IF NOT EXISTS media_assets_created_by_idx
    ON media_assets(created_by);
