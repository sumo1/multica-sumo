-- Task mode: PMO planning layer + discussion-phase chat.
--
-- default_planner_agent_id is the workspace's default PMO — the model the user
-- talks to in the task discussion phase, which plans the goal and assigns
-- roles. Nullable: when unset the backend falls back to the workspace's first
-- available agent. Per-task override can be added later.
ALTER TABLE workspace
    ADD COLUMN default_planner_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL;

-- A chat_session can be the discussion conversation for a goal_run (the
-- multi-round "form the requirement" chat with the PMO). Distinct from
-- goal_subtask_id (the takeover conversation for a single failed subtask).
-- Nullable: most chat sessions are neither.
ALTER TABLE chat_session
    ADD COLUMN goal_run_id UUID REFERENCES goal_run(id) ON DELETE SET NULL;

CREATE INDEX idx_chat_session_goal_run ON chat_session(goal_run_id);
