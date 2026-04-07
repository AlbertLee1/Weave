-- Reverse Tier 3.1 object_embeddings.
DROP INDEX IF EXISTS idx_object_embeddings_hnsw_cosine;
DROP INDEX IF EXISTS idx_object_embeddings_type_pk;
DROP TABLE IF EXISTS object_embeddings;
-- Intentionally do NOT drop the vector extension here: other tables may
-- depend on it, and dropping an extension cascades.
