package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// goalTestSquad creates a squad led by the workspace fixture agent and returns
// the squad id and the leader/fixture agent id. The fixture agent has an online
// cloud runtime, so dispatchSubtask passes its admission checks.
func goalTestSquad(t *testing.T, ctx context.Context, queries *db.Queries) (pgtype.UUID, pgtype.UUID) {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}
	squad, err := queries.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Name:        "goal-test-squad",
		Description: "goal DAG test squad",
		LeaderID:    parseUUID(agentID),
		CreatorID:   parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateSquad: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squad.ID)
	})
	return squad.ID, parseUUID(agentID)
}

// drainGoalSubtaskTask flips the most recent queued task for a subtask through
// dispatched → running and returns it, ready to be completed/failed. This
// simulates the daemon claiming and starting the task.
func drainGoalSubtaskTask(t *testing.T, ctx context.Context, queries *db.Queries, subtaskID pgtype.UUID) db.AgentTaskQueue {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent_task_queue WHERE goal_subtask_id = $1 AND status = 'queued' ORDER BY created_at DESC LIMIT 1`,
		subtaskID,
	).Scan(&taskID); err != nil {
		t.Fatalf("find queued task for subtask %s: %v", subtaskID, err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		taskID,
	); err != nil {
		t.Fatalf("mark task dispatched: %v", err)
	}
	task, err := queries.StartAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}
	return task
}

// TestGoalDAGHappyPath drives the full closed loop: a confirmed goal with two
// subtasks (B depends on A) dispatches A immediately, completing A unlocks and
// dispatches B, and completing B rolls the goal_run up to 'completed'.
func TestGoalDAGHappyPath(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)
	registerGoalListeners(bus, goalSvc)

	squadID, agentID := goalTestSquad(t, ctx, queries)

	run, subtasks, err := goalSvc.CreateGoal(
		ctx,
		parseUUID(testWorkspaceID), squadID, parseUUID(testUserID),
		pgtype.UUID{},
		"Refactor login", "Refactor the login module",
		[]service.SubtaskSpec{
			{Seq: 1, Title: "Backend API", Spec: "build the API", AssigneeAgentID: agentID},
			{Seq: 2, Title: "Frontend", Spec: "build the UI", AssigneeAgentID: agentID, DependsOn: []int32{1}},
		},
		true, // confirmed → executes immediately
	)
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, run.ID)
	})
	if len(subtasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(subtasks))
	}

	var subA, subB pgtype.UUID
	for _, st := range subtasks {
		switch st.Seq {
		case 1:
			subA = st.ID
		case 2:
			subB = st.ID
		}
	}

	// Subtask A (root) should be running; B should still be pending (blocked
	// behind its dependency, not yet dispatched).
	assertSubtaskStatus(t, ctx, queries, subA, "running")
	assertSubtaskStatus(t, ctx, queries, subB, "pending")
	if hasQueuedTask(t, ctx, subB) {
		t.Fatal("subtask B must not be dispatched before A completes")
	}

	// Complete A → listener fires → B unlocks and dispatches.
	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.CompleteTask(ctx, taskA.ID, []byte(`{"output":"api done"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask A: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subA, "completed")
	assertSubtaskStatus(t, ctx, queries, subB, "running")

	// Complete B → all subtasks terminal → PMO summary (收口) is dispatched and
	// the goal stays 'executing' until the summary lands.
	taskB := drainGoalSubtaskTask(t, ctx, queries, subB)
	if _, err := taskSvc.CompleteTask(ctx, taskB.ID, []byte(`{"output":"ui done"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask B: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subB, "completed")

	midRun, err := queries.GetGoalRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetGoalRun (mid): %v", err)
	}
	if midRun.Status != "executing" {
		t.Fatalf("expected goal to stay 'executing' pending summary, got %q", midRun.Status)
	}

	// Complete the PMO summary task → goal_run rolls up to 'completed'.
	summary := drainGoalSummaryTask(t, ctx, queries, run.ID)
	if _, err := taskSvc.CompleteTask(ctx, summary.ID, []byte(`{"output":"final deliverable"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask summary: %v", err)
	}

	finalRun, err := queries.GetGoalRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetGoalRun: %v", err)
	}
	if finalRun.Status != "completed" {
		t.Fatalf("expected goal_run status 'completed', got %q", finalRun.Status)
	}
}

// TestGoalDownstreamReceivesUpstreamOutput locks the data-flow fix: when an
// execute node A completes with a result, the downstream node B that depends on
// A must be dispatched with A's OUTPUT in its task context (upstream_output), so
// B builds on what A produced instead of re-deriving it. This is the bug seen in
// the UI where node 2 re-analyzed the project because node 1's result was never
// passed forward.
func TestGoalDownstreamReceivesUpstreamOutput(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)
	registerGoalListeners(bus, goalSvc)

	squadID, agentID := goalTestSquad(t, ctx, queries)

	run, subtasks, err := goalSvc.CreateGoal(
		ctx,
		parseUUID(testWorkspaceID), squadID, parseUUID(testUserID),
		pgtype.UUID{},
		"Naming", "Name the project",
		[]service.SubtaskSpec{
			{Seq: 1, Title: "Analyze essence", Spec: "distill the project essence", AssigneeAgentID: agentID},
			{Seq: 2, Title: "Generate names", Spec: "produce candidate names from node 1", AssigneeAgentID: agentID, DependsOn: []int32{1}},
		},
		true,
	)
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, run.ID)
	})

	var subA, subB pgtype.UUID
	for _, st := range subtasks {
		switch st.Seq {
		case 1:
			subA = st.ID
		case 2:
			subB = st.ID
		}
	}

	// Root A has no deps → no upstream_output.
	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	var ctxA service.GoalSubtaskContext
	_ = json.Unmarshal(taskA.Context, &ctxA)
	if ctxA.UpstreamOutput != "" {
		t.Fatalf("root node must have no upstream_output, got %q", ctxA.UpstreamOutput)
	}
	if ctxA.HandoffBrief != "" {
		t.Fatalf("root node must have no handoff_brief, got %q", ctxA.HandoffBrief)
	}

	// Complete A with a distinctive result → B unlocks + dispatches.
	const essence = "ESSENCE: a cross-project multi-agent task tool"
	if _, err := taskSvc.CompleteTask(ctx, taskA.ID, []byte(`{"output":"`+essence+`"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask A: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subB, "running")

	// B's dispatched task context MUST carry A's output as upstream_output.
	var ctxRawB []byte
	if err := testPool.QueryRow(ctx,
		`SELECT context FROM agent_task_queue WHERE goal_subtask_id=$1 ORDER BY created_at DESC LIMIT 1`, subB,
	).Scan(&ctxRawB); err != nil {
		t.Fatalf("load B task context: %v", err)
	}
	var ctxB service.GoalSubtaskContext
	if err := json.Unmarshal(ctxRawB, &ctxB); err != nil {
		t.Fatalf("unmarshal B context: %v", err)
	}
	if ctxB.UpstreamOutput == "" {
		t.Fatal("downstream node B dispatched WITHOUT upstream_output — the data-flow link is broken (B will re-derive node 1's work)")
	}
	if !strings.Contains(ctxB.UpstreamOutput, essence) {
		t.Fatalf("B's upstream_output must contain A's result %q, got %q", essence, ctxB.UpstreamOutput)
	}
	if !strings.Contains(ctxB.UpstreamOutput, "Analyze essence") {
		t.Fatalf("B's upstream_output should name the upstream node, got %q", ctxB.UpstreamOutput)
	}
	if ctxB.HandoffBrief == "" {
		t.Fatal("downstream node B dispatched WITHOUT handoff_brief — the runtime handoff is invisible to the executor")
	}
	if !strings.Contains(ctxB.HandoffBrief, "Task objective: Generate names") {
		t.Fatalf("B's handoff_brief should name the task objective, got %q", ctxB.HandoffBrief)
	}
	if !strings.Contains(ctxB.HandoffBrief, "intermediate handoff file") {
		t.Fatalf("B's handoff_brief should reject file-based handoff, got %q", ctxB.HandoffBrief)
	}
}

// drainGoalSummaryTask finds the PMO summary task dispatched for a goal (FK-less,
// context type goal_summary) and marks it started, mirroring drainGoalSubtaskTask.
func drainGoalSummaryTask(t *testing.T, ctx context.Context, queries *db.Queries, goalRunID pgtype.UUID) db.AgentTaskQueue {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent_task_queue
		 WHERE context::jsonb->>'type' = 'goal_summary'
		   AND context::jsonb->>'goal_run_id' = $1::text
		   AND status = 'queued' ORDER BY created_at DESC LIMIT 1`,
		util.UUIDToString(goalRunID),
	).Scan(&taskID); err != nil {
		t.Fatalf("find queued summary task for goal %s: %v", util.UUIDToString(goalRunID), err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		taskID,
	); err != nil {
		t.Fatalf("mark summary task dispatched: %v", err)
	}
	task, err := queries.StartAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatalf("StartAgentTask (summary): %v", err)
	}
	return task
}

// TestGoalDAGFailureAsksForJudgment locks in the 下一步判断 (next-step judgment)
// fork: when a subtask with downstream work fails AND a coordinator is available,
// the engine does NOT immediately block the downstream — it dispatches a decision
// task to the coordinator and leaves the dependent 'pending' (goal stays
// 'executing'). The coordinator's `abort` decision then reproduces the original
// block-downstream end-state. This is the Claude-Code-style judgment edge.
func TestGoalDAGFailureAsksForJudgment(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)
	registerGoalListeners(bus, goalSvc)

	squadID, agentID := goalTestSquad(t, ctx, queries)

	run, subtasks, err := goalSvc.CreateGoal(
		ctx,
		parseUUID(testWorkspaceID), squadID, parseUUID(testUserID),
		pgtype.UUID{},
		"Risky goal", "A goal whose first step fails",
		[]service.SubtaskSpec{
			{Seq: 1, Title: "Flaky step", Spec: "this fails", AssigneeAgentID: agentID},
			{Seq: 2, Title: "Downstream", Spec: "depends on flaky", AssigneeAgentID: agentID, DependsOn: []int32{1}},
		},
		true,
	)
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, run.ID)
	})

	var subA, subB pgtype.UUID
	for _, st := range subtasks {
		switch st.Seq {
		case 1:
			subA = st.ID
		case 2:
			subB = st.ID
		}
	}

	// Force max_attempts=1 on A so the failure is terminal immediately.
	if _, err := testPool.Exec(ctx,
		`UPDATE goal_subtask SET max_attempts = 1 WHERE id = $1`, subA,
	); err != nil {
		t.Fatalf("set max_attempts: %v", err)
	}

	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.FailTask(ctx, taskA.ID, "lint error", "", "", "agent_error"); err != nil {
		t.Fatalf("FailTask A: %v", err)
	}

	// A failed; B is NOT blocked yet — the coordinator was asked to judge first.
	assertSubtaskStatus(t, ctx, queries, subA, "failed")
	assertSubtaskStatus(t, ctx, queries, subB, "pending")

	// A goal_decision task must have been dispatched to the coordinator.
	var decisionTaskID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent_task_queue
		   WHERE context::jsonb->>'type' = 'goal_decision'
		     AND context::jsonb->>'goal_subtask_id' = $1::text
		   ORDER BY created_at DESC LIMIT 1`,
		util.UUIDToString(subA),
	).Scan(&decisionTaskID); err != nil {
		t.Fatalf("expected a goal_decision task for the failed node: %v", err)
	}

	// Goal stays executing while the judgment is pending.
	midRun, _ := queries.GetGoalRun(ctx, run.ID)
	if midRun.Status != "executing" {
		t.Fatalf("goal should stay 'executing' while judgment pends, got %q", midRun.Status)
	}

	// Coordinator decides 'abort' → downstream blocked, goal rolls up to failed
	// (the original end-state, now reached through the judgment path).
	if _, err := goalSvc.DecideSubtask(ctx, parseUUID(testWorkspaceID), subA, "abort", ""); err != nil {
		t.Fatalf("DecideSubtask abort: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subB, "blocked")
	finalRun, err := queries.GetGoalRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetGoalRun: %v", err)
	}
	if finalRun.Status != "failed" {
		t.Fatalf("expected goal_run status 'failed' after abort, got %q", finalRun.Status)
	}
}

// TestGoalDecisionProceedUnblocksDownstream locks the 'proceed' enactment: the
// coordinator judges a non-fatal failure → the failed node is skipped and its
// downstream runs (the goal continues past a node that failed).
func TestGoalDecisionProceedUnblocksDownstream(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)
	registerGoalListeners(bus, goalSvc)

	squadID, agentID := goalTestSquad(t, ctx, queries)

	run, subtasks, err := goalSvc.CreateGoal(
		ctx,
		parseUUID(testWorkspaceID), squadID, parseUUID(testUserID),
		pgtype.UUID{},
		"Proceed goal", "first step is optional",
		[]service.SubtaskSpec{
			{Seq: 1, Title: "Optional step", Spec: "may fail", AssigneeAgentID: agentID},
			{Seq: 2, Title: "Downstream", Spec: "runs regardless", AssigneeAgentID: agentID, DependsOn: []int32{1}},
		},
		true,
	)
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, run.ID)
	})

	var subA, subB pgtype.UUID
	for _, st := range subtasks {
		switch st.Seq {
		case 1:
			subA = st.ID
		case 2:
			subB = st.ID
		}
	}
	if _, err := testPool.Exec(ctx, `UPDATE goal_subtask SET max_attempts = 1 WHERE id = $1`, subA); err != nil {
		t.Fatalf("set max_attempts: %v", err)
	}

	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.FailTask(ctx, taskA.ID, "soft failure", "", "", "agent_error"); err != nil {
		t.Fatalf("FailTask A: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subB, "pending") // judgment pending

	// Coordinator decides 'proceed' → A skipped, B unblocked and dispatched.
	if _, err := goalSvc.DecideSubtask(ctx, parseUUID(testWorkspaceID), subA, "proceed", ""); err != nil {
		t.Fatalf("DecideSubtask proceed: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subA, "skipped")
	assertSubtaskStatus(t, ctx, queries, subB, "running")
}

// TestGoalDecisionFailSafeBlocksOnAbandonedJudgment locks the listener fail-safe:
// if the decision task ends WITHOUT the coordinator reporting a verdict (the node
// is still 'failed'), SyncDecisionFromTask degrades to blocking the downstream —
// the goal never strands in 'executing' waiting on a verdict that never comes.
func TestGoalDecisionFailSafeBlocksOnAbandonedJudgment(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)
	registerGoalListeners(bus, goalSvc)

	squadID, agentID := goalTestSquad(t, ctx, queries)

	run, subtasks, err := goalSvc.CreateGoal(
		ctx,
		parseUUID(testWorkspaceID), squadID, parseUUID(testUserID),
		pgtype.UUID{},
		"Abandoned judgment goal", "coordinator never decides",
		[]service.SubtaskSpec{
			{Seq: 1, Title: "Flaky step", Spec: "fails", AssigneeAgentID: agentID},
			{Seq: 2, Title: "Downstream", Spec: "depends", AssigneeAgentID: agentID, DependsOn: []int32{1}},
		},
		true,
	)
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, run.ID)
	})

	var subA, subB pgtype.UUID
	for _, st := range subtasks {
		switch st.Seq {
		case 1:
			subA = st.ID
		case 2:
			subB = st.ID
		}
	}
	if _, err := testPool.Exec(ctx, `UPDATE goal_subtask SET max_attempts = 1 WHERE id = $1`, subA); err != nil {
		t.Fatalf("set max_attempts: %v", err)
	}

	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.FailTask(ctx, taskA.ID, "boom", "", "", "agent_error"); err != nil {
		t.Fatalf("FailTask A: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subB, "pending")

	// Find the decision task, then complete it WITHOUT any `goal decide` call.
	var decisionTaskID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent_task_queue
		   WHERE context::jsonb->>'type' = 'goal_decision'
		     AND context::jsonb->>'goal_subtask_id' = $1::text
		   ORDER BY created_at DESC LIMIT 1`,
		util.UUIDToString(subA),
	).Scan(&decisionTaskID); err != nil {
		t.Fatalf("find decision task: %v", err)
	}
	// Drive it dispatched→running→completed (simulating a leader that finished
	// but forgot to report). CompleteTask fires EventTaskCompleted → listener.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status='dispatched', dispatched_at=now() WHERE id=$1`, decisionTaskID,
	); err != nil {
		t.Fatalf("dispatch decision task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, parseUUID(decisionTaskID)); err != nil {
		t.Fatalf("start decision task: %v", err)
	}
	if _, err := taskSvc.CompleteTask(ctx, parseUUID(decisionTaskID), []byte(`{"output":"forgot to decide"}`), "", ""); err != nil {
		t.Fatalf("complete decision task: %v", err)
	}

	// Fail-safe kicked in: downstream blocked, goal rolled up to failed.
	assertSubtaskStatus(t, ctx, queries, subB, "blocked")
	finalRun, _ := queries.GetGoalRun(ctx, run.ID)
	if finalRun.Status != "failed" {
		t.Fatalf("expected 'failed' after abandoned judgment, got %q", finalRun.Status)
	}
}

// TestGoalDAGFailureBlocksWithoutCoordinator locks the fail-safe: when NO usable
// coordinator exists (leader archived), a failure degrades to the original
// behavior — block the downstream immediately, no judgment task, goal → failed.
// The worst case is never a stuck goal.
func TestGoalDAGFailureBlocksWithoutCoordinator(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)
	registerGoalListeners(bus, goalSvc)

	squadID, agentID := goalTestSquad(t, ctx, queries)

	run, subtasks, err := goalSvc.CreateGoal(
		ctx,
		parseUUID(testWorkspaceID), squadID, parseUUID(testUserID),
		pgtype.UUID{},
		"Risky goal (no coordinator)", "fails with the leader gone",
		[]service.SubtaskSpec{
			{Seq: 1, Title: "Flaky step", Spec: "this fails", AssigneeAgentID: agentID},
			{Seq: 2, Title: "Downstream", Spec: "depends on flaky", AssigneeAgentID: agentID, DependsOn: []int32{1}},
		},
		true,
	)
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, run.ID)
	})

	var subA, subB pgtype.UUID
	for _, st := range subtasks {
		switch st.Seq {
		case 1:
			subA = st.ID
		case 2:
			subB = st.ID
		}
	}
	if _, err := testPool.Exec(ctx, `UPDATE goal_subtask SET max_attempts = 1 WHERE id = $1`, subA); err != nil {
		t.Fatalf("set max_attempts: %v", err)
	}

	// Archive the squad leader so no coordinator can judge. Restore after.
	leaderID := agentID
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, util.UUIDToString(leaderID)); err != nil {
		t.Fatalf("archive leader: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `UPDATE agent SET archived_at = NULL WHERE id = $1`, util.UUIDToString(leaderID))
	})

	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.FailTask(ctx, taskA.ID, "lint error", "", "", "agent_error"); err != nil {
		t.Fatalf("FailTask A: %v", err)
	}

	// No coordinator → immediate block (original behavior), no decision task.
	assertSubtaskStatus(t, ctx, queries, subA, "failed")
	assertSubtaskStatus(t, ctx, queries, subB, "blocked")
	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue
		   WHERE context::jsonb->>'type' = 'goal_decision'
		     AND context::jsonb->>'goal_subtask_id' = $1::text`,
		util.UUIDToString(subA),
	).Scan(&n); err != nil {
		t.Fatalf("count decision tasks: %v", err)
	}
	if n != 0 {
		t.Fatalf("no decision task should be dispatched without a coordinator, got %d", n)
	}
	finalRun, _ := queries.GetGoalRun(ctx, run.ID)
	if finalRun.Status != "failed" {
		t.Fatalf("expected goal_run status 'failed', got %q", finalRun.Status)
	}
}

func assertSubtaskStatus(t *testing.T, ctx context.Context, queries *db.Queries, id pgtype.UUID, want string) {
	t.Helper()
	st, err := queries.GetGoalSubtask(ctx, id)
	if err != nil {
		t.Fatalf("GetGoalSubtask: %v", err)
	}
	if st.Status != want {
		t.Fatalf("subtask %s: expected status %q, got %q", id, want, st.Status)
	}
}

func hasQueuedTask(t *testing.T, ctx context.Context, subtaskID pgtype.UUID) bool {
	t.Helper()
	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE goal_subtask_id = $1`,
		subtaskID,
	).Scan(&n); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return n > 0
}
