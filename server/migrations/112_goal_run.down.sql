DROP INDEX IF EXISTS idx_agent_task_queue_goal_subtask;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS goal_subtask_id;

DROP TABLE IF EXISTS goal_subtask;
DROP TABLE IF EXISTS goal_run;
