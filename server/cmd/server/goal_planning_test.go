package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestGoalStartPlanningDispatchesToLeader verifies the LLM-decomposition entry
// point: StartPlanning creates a goal in 'planning' status and enqueues a
// planning task for the squad leader, carrying the goal context so the daemon
// can build the planning prompt.
func TestGoalStartPlanningDispatchesToLeader(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)

	squadID, leaderID := goalTestSquad(t, ctx, queries)

	run, err := goalSvc.StartPlanning(
		ctx,
		parseUUID(testWorkspaceID), squadID, parseUUID(testUserID),
		pgtype.UUID{},
		"Refactor login", "Refactor the whole login module",
	)
	if err != nil {
		t.Fatalf("StartPlanning: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, run.ID)
	})

	if run.Status != "planning" {
		t.Fatalf("expected goal status 'planning', got %q", run.Status)
	}

	// A planning task should be queued for the leader, with a goal_planning
	// context blob (no goal_subtask_id, no issue/chat/autopilot link).
	var taskCtx []byte
	if err := testPool.QueryRow(ctx, `
		SELECT context FROM agent_task_queue
		WHERE agent_id = $1 AND status = 'queued'
		  AND goal_subtask_id IS NULL AND issue_id IS NULL
		  AND chat_session_id IS NULL AND autopilot_run_id IS NULL
		ORDER BY created_at DESC LIMIT 1
	`, leaderID).Scan(&taskCtx); err != nil {
		t.Fatalf("find planning task: %v", err)
	}

	var gc service.GoalPlanningContext
	if err := json.Unmarshal(taskCtx, &gc); err != nil {
		t.Fatalf("unmarshal planning context: %v", err)
	}
	if gc.Type != service.GoalPlanningContextType {
		t.Fatalf("expected context type %q, got %q", service.GoalPlanningContextType, gc.Type)
	}
	if gc.GoalRunID == "" || gc.Goal == "" {
		t.Fatalf("planning context missing goal_run_id/goal: %+v", gc)
	}
}

// TestGoalSubmitPlanRunsDAG verifies the write-back: SubmitPlan persists the
// leader's decomposition, flips the goal to executing, and dispatches roots —
// the same closed loop as the explicit-plan path, but reached via planning.
func TestGoalSubmitPlanRunsDAG(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)
	registerGoalListeners(bus, goalSvc)

	squadID, leaderID := goalTestSquad(t, ctx, queries)

	run, err := goalSvc.StartPlanning(
		ctx,
		parseUUID(testWorkspaceID), squadID, parseUUID(testUserID),
		pgtype.UUID{},
		"Ship feature", "Build and test feature X",
	)
	if err != nil {
		t.Fatalf("StartPlanning: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, run.ID)
	})

	// Leader submits a 2-node plan: B depends on A. Both assigned to the leader
	// agent (the fixture squad's only online agent).
	updated, created, err := goalSvc.SubmitPlan(ctx, run.ID, []service.SubtaskSpec{
		{Seq: 1, Title: "Build", Spec: "build it", AssigneeAgentID: leaderID},
		{Seq: 2, Title: "Test", Spec: "test it", AssigneeAgentID: leaderID, DependsOn: []int32{1}},
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if updated.Status != "executing" {
		t.Fatalf("expected goal status 'executing' after submit, got %q", updated.Status)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(created))
	}

	var subA, subB pgtype.UUID
	for _, st := range created {
		switch st.Seq {
		case 1:
			subA = st.ID
		case 2:
			subB = st.ID
		}
	}

	// Root (A) dispatched → running; dependent (B) still pending.
	assertSubtaskStatus(t, ctx, queries, subA, "running")
	assertSubtaskStatus(t, ctx, queries, subB, "pending")

	// Complete A → B unlocks and dispatches (closed loop from the planning path).
	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.CompleteTask(ctx, taskA.ID, []byte(`{"output":"built"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask A: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subA, "completed")
	assertSubtaskStatus(t, ctx, queries, subB, "running")
}

// TestGoalPlanningFailureFailsGoal locks in the real-machine fix: when the
// planning task fails (e.g. the leader agent errors out) the goal must roll up
// to 'failed' instead of being stuck in 'planning' forever. Found via live run
// 2026-06-09 (agent hit a 403 credential error; goal hung in planning).
func TestGoalPlanningFailureFailsGoal(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)
	registerGoalListeners(bus, goalSvc)

	squadID, _ := goalTestSquad(t, ctx, queries)

	run, err := goalSvc.StartPlanning(
		ctx,
		parseUUID(testWorkspaceID), squadID, parseUUID(testUserID),
		pgtype.UUID{},
		"Doomed goal", "A goal whose planning agent errors out",
	)
	if err != nil {
		t.Fatalf("StartPlanning: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, run.ID)
	})
	if run.Status != "planning" {
		t.Fatalf("expected 'planning', got %q", run.Status)
	}

	// Find the planning task and drive it to failed through the real lifecycle.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		SELECT id::text FROM agent_task_queue
		WHERE context::jsonb->>'goal_run_id' = $1
		  AND context::jsonb->>'type' = 'goal_planning'
		ORDER BY created_at DESC LIMIT 1
	`, run.ID).Scan(&taskID); err != nil {
		t.Fatalf("find planning task: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status='dispatched', dispatched_at=now() WHERE id=$1`, taskID,
	); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, parseUUID(taskID)); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}
	if _, err := taskSvc.FailTask(ctx, parseUUID(taskID), "403 no subscription", "", "", "agent_error"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	final, err := queries.GetGoalRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetGoalRun: %v", err)
	}
	if final.Status != "failed" {
		t.Fatalf("planning failure must fail the goal, got %q", final.Status)
	}
	if !final.FailureReason.Valid || final.FailureReason.String == "" {
		t.Fatalf("failed goal must carry a reason")
	}
}
