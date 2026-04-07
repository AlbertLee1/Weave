-- Tier 3.1: pgvector-backed object embeddings for nearestNeighbors search.
--
-- Each row stores a vector embedding for a single (objectTypeRID, primaryKey)
-- tuple under a specific model. Multiple models can coexist for the same
-- object so we can A/B compare or migrate between models without losing data.
--
-- The 1536 dimensionality matches OpenAI ada-002 / text-embedding-3-small.
-- Larger / smaller models would require either a separate table or a column
-- of a different `vector(N)` type.
--
-- IMPORTANT: this migration depends on the pgvector extension. The
-- CREATE EXTENSION call below is idempotent, but on hosts where pgvector is
-- not installed it will fail outright. In that case the operator must either
-- install pgvector (the recommended path) or skip this migration entirely.
-- The repository code degrades gracefully when no rows exist, so the rest of
-- the system remains functional with vector search disabled.

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS object_embeddings (
    id              BIGSERIAL PRIMARY KEY,
    object_type_rid TEXT NOT NULL,
    primary_key     TEXT NOT NULL,
    embedding       vector(1536),
    model           TEXT NOT NULL,
    source_text     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (object_type_rid, primary_key, model)
);

CREATE INDEX IF NOT EXISTS idx_object_embeddings_type_pk
    ON object_embeddings (object_type_rid, primary_key);

-- HNSW index for fast approximate nearest neighbor search using cosine
-- similarity. Created conditionally so the migration can run on test hosts
-- where pgvector is loaded but hnsw is unavailable for some reason.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector') THEN
        CREATE INDEX IF NOT EXISTS idx_object_embeddings_hnsw_cosine
        ON object_embeddings USING hnsw (embedding vector_cosine_ops);
    END IF;
END $$;
