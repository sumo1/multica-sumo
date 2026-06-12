-- Goal mode: PMO-orchestrated multi-role execution (migration 112).
-- goal_run is the orchestration container (one goal); goal_subtask rows form
-- a DAG. Execution reuses agent_task_queue via its goal_subtask_id column.

-- name: CreateGoalRun :one
INSERT INTO goal_run (
    workspace_id, squad_id, chat_session_id, creator_id, title, goal, status,
    project_id
) VALUES (
    $1, $2, sqlc.narg('chat_session_id'), $3, $4, $5,
    COALESCE(sqlc.narg('status'), 'discussion'),
    sqlc.narg('project_id')
) RETURNING *;

-- name: GetGoalRun :one
SELECT * FROM goal_run
WHERE id = $1;

-- name: GetGoalRunInWorkspace :one
SELECT * FROM goal_run
WHERE id = $1 AND workspace_id = $2;

-- name: ListGoalRunsForWorkspace :many
-- Task list for the Task page: all goals in a workspace, newest first.
SELECT * FROM goal_run
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListGoalRunsForSquad :many
SELECT * FROM goal_run
WHERE squad_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetGoalRunByChatSession :one
SELECT * FROM goal_run
WHERE chat_session_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateGoalRunGoal :one
-- Discussion phase: refine title/goal as the requirement converges.
UPDATE goal_run
SET title = $2, goal = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetGoalRunChatSession :one
-- Link a goal to its discussion chat session (task mode: chat is created after
-- the goal so the goal can reference it).
UPDATE goal_run
SET chat_session_id = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateGoalRunStatus :one
UPDATE goal_run
SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ConfirmGoalRun :one
-- Confirm gate: discussion -> confirmed. Stamps confirmed_at.
UPDATE goal_run
SET status = 'confirmed', confirmed_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CompleteGoalRun :one
UPDATE goal_run
SET status = $2, completed_at = now(),
    failure_reason = sqlc.narg('failure_reason'), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateGoalSubtask :one
INSERT INTO goal_subtask (
    goal_run_id, seq, title, spec, assignee_agent_id, depends_on,
    status, max_attempts, kind
) VALUES (
    $1, $2, $3, $4, sqlc.narg('assignee_agent_id'),
    COALESCE(sqlc.narg('depends_on'), '{}')::uuid[],
    COALESCE(sqlc.narg('status'), 'pending'),
    COALESCE(sqlc.narg('max_attempts'), 2),
    COALESCE(sqlc.narg('kind'), 'execute')
) RETURNING *;

-- name: GetGoalSubtask :one
SELECT * FROM goal_subtask
WHERE id = $1;

-- name: ListGoalSubtasks :many
SELECT * FROM goal_subtask
WHERE goal_run_id = $1
ORDER BY seq ASC, created_at ASC;

-- name: UpdateGoalSubtaskStatus :one
UPDATE goal_subtask
SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetGoalSubtaskDependsOn :one
-- Second-pass DAG wiring: set the depends_on UUID array after all sibling
-- subtasks exist and their ids are known.
UPDATE goal_subtask
SET depends_on = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: StartGoalSubtask :one
-- ready/pending -> running. Increments attempt, stamps started_at.
UPDATE goal_subtask
SET status = 'running', attempt = attempt + 1,
    started_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CompleteGoalSubtask :one
UPDATE goal_subtask
SET status = 'completed', result = sqlc.narg('result'),
    completed_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: FailGoalSubtask :one
UPDATE goal_subtask
SET status = 'failed', failure_reason = $2,
    completed_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetGoalSubtaskVerdict :one
-- Record a verify node's verdict (pass/reject) and mark it completed. The
-- scheduler reads the verdict to either unblock downstream (pass) or re-run
-- the reviewed node (reject).
UPDATE goal_subtask
SET verdict = $2, status = 'completed',
    result = sqlc.narg('result'), completed_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RearmGoalSubtask :one
-- Reset a node back to 'ready' for another run (reject bounce / re-verify),
-- clearing the terminal timestamps and prior verdict so it dispatches cleanly.
UPDATE goal_subtask
SET status = 'ready', verdict = NULL, completed_at = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RearmGoalSubtaskFresh :one
-- Manual-intervention rearm: like RearmGoalSubtask but also resets attempt to 0
-- and clears failure_reason. A human-triggered retry/reassign/edit-spec gets a
-- fresh auto-retry budget — it is not bounded by the budget the auto path
-- already exhausted (which is what escalated to the user in the first place).
UPDATE goal_subtask
SET status = 'ready', verdict = NULL, completed_at = NULL,
    attempt = 0, failure_reason = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SkipGoalSubtask :one
-- "跳过" intervention: mark a failed/blocked node skipped. The terminal handler
-- treats skipped like a non-blocking terminal so downstream can proceed.
UPDATE goal_subtask
SET status = 'skipped', completed_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateGoalSubtaskSpec :one
-- "编辑 spec" intervention: edit a failed node's spec before retry.
UPDATE goal_subtask
SET spec = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ReassignGoalSubtask :one
-- "改派角色" intervention: swap the executing agent.
UPDATE goal_subtask
SET assignee_agent_id = sqlc.narg('assignee_agent_id'), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateGoalSubtaskTask :one
-- Dispatch a subtask: enqueue an agent_task_queue row linked back via
-- goal_subtask_id. Mirrors CreateQuickCreateTask (no issue link, prompt/spec
-- carried in context JSONB) so the daemon executes it on the free-prompt path.
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, context, goal_subtask_id
) VALUES ($1, $2, NULL, 'queued', $3, $4, $5)
RETURNING *;

-- name: GetLatestTaskForSubtask :one
-- The most recent execution task for a goal subtask (to fetch its task_messages
-- stream / surface execution output in the Task page ④ column).
SELECT id FROM agent_task_queue
WHERE goal_subtask_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: GetPlanningTaskForGoal :one
-- The most recent planning task for a goal_run (PMO decomposition), found via
-- its context JSONB. Used to show the main/PMO session stream in ④.
SELECT id FROM agent_task_queue
WHERE context::jsonb->>'type' = 'goal_planning'
  AND context::jsonb->>'goal_run_id' = $1::text
ORDER BY created_at DESC
LIMIT 1;

-- name: GetSummaryTaskForGoal :one
-- The most recent PMO summary task for a goal_run (final 收口/汇总), found via
-- its context JSONB. Used both as an idempotency guard (don't dispatch twice)
-- and to surface the summary stream as the tail of the main/PMO session in ④.
SELECT id FROM agent_task_queue
WHERE context::jsonb->>'type' = 'goal_summary'
  AND context::jsonb->>'goal_run_id' = $1::text
ORDER BY created_at DESC
LIMIT 1;

-- name: GetPersistTaskForGoal :one
-- The most recent repo-persist task for a goal_run (one-click snapshot to the
-- bound project repo), found via its context JSONB. Used to surface the persist
-- stream in the main/PMO session. Persist is repeatable (snapshot overwrite), so
-- this is NOT an idempotency guard — it only finds the latest run for display.
SELECT id FROM agent_task_queue
WHERE context::jsonb->>'type' = 'goal_persist'
  AND context::jsonb->>'goal_run_id' = $1::text
ORDER BY created_at DESC
LIMIT 1;

-- name: GetActiveDecisionTaskForSubtask :one
-- A not-yet-terminal 总控 decision task ("下一步判断") for the given failed
-- subtask, found via its context JSONB. Used as an idempotency guard so a single
-- failure dispatches at most one decision task while it is in flight.
SELECT id FROM agent_task_queue
WHERE context::jsonb->>'type' = 'goal_decision'
  AND context::jsonb->>'goal_subtask_id' = $1::text
  AND status IN ('queued', 'dispatched', 'running')
ORDER BY created_at DESC
LIMIT 1;

-- name: ListGoalSubtaskDependents :many
-- Subtasks that declare the given subtask id in their depends_on array.
SELECT * FROM goal_subtask
WHERE goal_run_id = $1 AND sqlc.arg('dependency_id')::uuid = ANY(depends_on)
ORDER BY seq ASC;
