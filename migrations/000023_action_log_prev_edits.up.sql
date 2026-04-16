-- US-104: Add prev_edits column to action_logs for undo/PrevState recording.
ALTER TABLE action_logs ADD COLUMN IF NOT EXISTS prev_edits JSONB;
