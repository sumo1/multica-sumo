DROP INDEX IF EXISTS idx_goal_run_project;
ALTER TABLE goal_run DROP COLUMN IF EXISTS project_id;
