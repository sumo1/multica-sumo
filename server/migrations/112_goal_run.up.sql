-- Goal mode: PMO-orchestrated multi-role execution on top of a squad.
-- A goal_run is one orchestration container (the "goal"); it decomposes into
-- goal_subtask rows forming a DAG. Distinct from autopilot_run, which is 1:1
-- task and bound to an autopilot (autopilot_id NOT NULL) — goal is 1:N and
-- bound to a squad + chat_session, so it gets its own tables (no semantic
-- overload, no breaking the autopilot CHECK/NOT NULL constraints).

CREATE TABLE goal_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    -- The team executing this goal. PMO = squad.leader_id; roles = squad_member.
    squad_id UUID NOT NULL REFERENCES squad(id) ON DELETE CASCADE,
    -- The conversation this goal is driven from (discussion → confirm → execute).
    chat_session_id UUID REFERENCES chat_session(id) ON DELETE SET NULL,
    creator_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    -- The agreed goal text (requirement summary). Full requirement doc lives in
    -- the target repo's docs/task/{goal-id}/, this is just the in-platform handle.
    goal TEXT NOT NULL DEFAULT '',
    -- Lifecycle: discussion (multi-round w/ PMO) → confirmed (gate passed) →
    -- planning (PMO decomposing) → executing → completed/failed/cancelled.
    -- 'partial' = some subtasks done, downstream blocked by a failure.
    status TEXT NOT NULL DEFAULT 'discussion'
        CHECK (status IN ('discussion', 'confirmed', 'planning', 'executing',
                          'completed', 'partial', 'failed', 'cancelled')),
    confirmed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_goal_run_workspace ON goal_run(workspace_id);
CREATE INDEX idx_goal_run_squad ON goal_run(squad_id);
CREATE INDEX idx_goal_run_chat_session ON goal_run(chat_session_id);

-- Subtasks form a DAG. depends_on holds goal_subtask ids that must reach a
-- terminal-success state before this one can be dispatched. Empty array = root,
-- dispatchable immediately. assignee = the role agent that executes this node.
CREATE TABLE goal_subtask (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    goal_run_id UUID NOT NULL REFERENCES goal_run(id) ON DELETE CASCADE,
    -- Display order within the goal (PMO decomposition order).
    seq INT NOT NULL DEFAULT 0,
    title TEXT NOT NULL DEFAULT '',
    -- The subtask spec (what this role must achieve). Editable on failure
    -- escalation ("编辑 spec" intervention).
    spec TEXT NOT NULL DEFAULT '',
    -- Executing role agent. Nullable until PMO assigns during planning.
    assignee_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL,
    -- DAG edges: ids of sibling goal_subtask rows this depends on.
    depends_on UUID[] NOT NULL DEFAULT '{}',
    -- pending → ready (deps satisfied) → running → completed/failed.
    -- 'blocked' = an upstream dependency failed (⊘ in the tree).
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'ready', 'running', 'completed',
                          'failed', 'blocked', 'skipped')),
    attempt INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 2,
    failure_reason TEXT,
    result JSONB,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_goal_subtask_run ON goal_subtask(goal_run_id);
CREATE INDEX idx_goal_subtask_assignee ON goal_subtask(assignee_agent_id);

-- Execution reuses the existing task queue. A subtask dispatch creates an
-- agent_task_queue row linked back here, mirroring autopilot_run_id wiring,
-- so WS task:* events map straight onto the tree node.
ALTER TABLE agent_task_queue
    ADD COLUMN goal_subtask_id UUID REFERENCES goal_subtask(id) ON DELETE SET NULL;

CREATE INDEX idx_agent_task_queue_goal_subtask ON agent_task_queue(goal_subtask_id);
