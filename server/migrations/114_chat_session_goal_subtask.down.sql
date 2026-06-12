DROP INDEX IF EXISTS idx_chat_session_goal_subtask;
ALTER TABLE chat_session DROP COLUMN IF EXISTS goal_subtask_id;
