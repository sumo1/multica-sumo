DROP INDEX IF EXISTS idx_chat_session_goal_run;
ALTER TABLE chat_session DROP COLUMN IF EXISTS goal_run_id;
ALTER TABLE workspace DROP COLUMN IF EXISTS default_planner_agent_id;
