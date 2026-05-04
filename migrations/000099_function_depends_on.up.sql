-- US-425: Function 缓存失效事件. Authors that flag a Function as `pure=true`
-- (US-221) get an LRU+TTL cache; when the function's output depends on the
-- live state of one or more ObjectTypes, an edit to those types must
-- invalidate the cached results so a stale answer cannot linger past the
-- next mutation.
--
-- `depends_on` is a multivalued list of ObjectType API names. Each name is
-- the canonical wire identifier already used by edits (`Edit.ObjectType` in
-- pkg/funnel/types.go). On an applied EditBatch the cache invalidator
-- consults this column to decide which Function entries to drop.
--
-- Default '{}' keeps legacy rows in the no-dependency bucket: the cache
-- still respects the 5-minute TTL ceiling, but no extra invalidation fires
-- on object change.

ALTER TABLE functions
    ADD COLUMN IF NOT EXISTS depends_on TEXT[] NOT NULL DEFAULT '{}';
