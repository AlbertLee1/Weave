-- Add result column to automation_executions for storing function effect outputs
ALTER TABLE automation_executions ADD COLUMN result JSONB;
