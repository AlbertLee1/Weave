-- US-221: Function 结果缓存. Authors flag a Function as `pure=true` when its
-- output is fully determined by its input (no I/O, no clock reads, no
-- mutation). The execute handler then keys an LRU+TTL cache on
-- `rid@version + hash(params)` and short-circuits future calls within the
-- 5-minute window.
--
-- Default FALSE keeps every existing row firmly in the no-cache bucket so
-- functions that quietly read external state (HTTP, timestamps, ...) keep
-- their original semantics — opting in is a deliberate authoring choice.

ALTER TABLE functions ADD COLUMN IF NOT EXISTS pure BOOLEAN NOT NULL DEFAULT FALSE;
