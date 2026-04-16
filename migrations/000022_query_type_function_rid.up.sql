-- US-094: Add function_rid column to query_types for function-backed query execution.
ALTER TABLE query_types ADD COLUMN IF NOT EXISTS function_rid TEXT DEFAULT '';
