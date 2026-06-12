package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// goalVerifyWorkflow builds a confirmed goal: execute A → verify V(reviews A) →
// execute B(after V). Returns the service, queries, and the three subtask ids.
func goalVerifyWorkflow(t *testing.T, ctx context.Context) (
	*service.GoalService, *service.TaskService, *db.Queries,
	db.GoalRun, pgtype.UUID, pgtype.UUID, pgtype.UUID,
) {
	t.Helper()
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
		"Verified goal", "Build with adversarial review",
		[]service.SubtaskSpec{
			{Seq: 1, Title: "Build", Spec: "build it", AssigneeAgentID: agentID},
			{Seq: 2, Title: "Review", Spec: "review the build", AssigneeAgentID: agentID, DependsOn: []int32{1}, Kind: "verify"},
			{Seq: 3, Title: "Ship", Spec: "ship it", AssigneeAgentID: agentID, DependsOn: []int32{2}},
		},
		true,
	)
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, run.ID)
	})

	var a, v, b pgtype.UUID
	for _, st := range subtasks {
		switch st.Seq {
		case 1:
			a = st.ID
		case 2:
			v = st.ID
		case 3:
			b = st.ID
		}
	}
	return goalSvc, taskSvc, queries, run, a, v, b
}

// TestGoalVerifyPassUnblocksDownstream: A completes → verify V dispatched → V
// passes → V completed → B (downstream of V) dispatches.
func TestGoalVerifyPassUnblocksDownstream(t *testing.T) {
	ctx := context.Background()
	goalSvc, taskSvc, queries, _, subA, subV, subB := goalVerifyWorkflow(t, ctx)

	// A is the only root → running. V and B pending.
	assertSubtaskStatus(t, ctx, queries, subA, "running")
	assertSubtaskStatus(t, ctx, queries, subV, "pending")
	assertSubtaskStatus(t, ctx, queries, subB, "pending")

	// Complete A → verify V unlocks + dispatches.
	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.CompleteTask(ctx, taskA.ID, []byte(`{"output":"built"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask A: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subA, "completed")
	assertSubtaskStatus(t, ctx, queries, subV, "running")
	assertSubtaskStatus(t, ctx, queries, subB, "pending")

	// Verifier reports pass (via the service, as the CLI would), then its task
	// completes → V finalized completed, B unlocks.
	if _, err := goalSvc.SubmitVerdict(ctx, parseUUID(testWorkspaceID), subV, "pass", ""); err != nil {
		t.Fatalf("SubmitVerdict pass: %v", err)
	}
	taskV := drainGoalSubtaskTask(t, ctx, queries, subV)
	if _, err := taskSvc.CompleteTask(ctx, taskV.ID, []byte(`{"verdict":"pass"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask V: %v", err)
	}

	vRow, _ := queries.GetGoalSubtask(ctx, subV)
	if vRow.Status != "completed" || !vRow.Verdict.Valid || vRow.Verdict.String != "pass" {
		t.Fatalf("verify node: expected completed/pass, got %s/%v", vRow.Status, vRow.Verdict)
	}
	assertSubtaskStatus(t, ctx, queries, subB, "running")

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
	if !strings.Contains(ctxB.UpstreamOutput, "built") {
		t.Fatalf("B's source material should include the reviewed producer output, got %q", ctxB.UpstreamOutput)
	}
	if !strings.Contains(ctxB.UpstreamOutput, "Verdict: pass") {
		t.Fatalf("B's source material should include the verifier verdict, got %q", ctxB.UpstreamOutput)
	}
}

// TestGoalVerifyRejectRerunsReviewed: A completes → V dispatched → V rejects →
// A re-runs (attempt bumped), V re-arms, B stays blocked behind V.
func TestGoalVerifyRejectRerunsReviewed(t *testing.T) {
	ctx := context.Background()
	goalSvc, taskSvc, queries, _, subA, subV, subB := goalVerifyWorkflow(t, ctx)

	// Drive A → V running.
	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.CompleteTask(ctx, taskA.ID, []byte(`{"output":"v1"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask A: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subV, "running")

	aBefore, _ := queries.GetGoalSubtask(ctx, subA)

	// Verifier rejects → A bounced back to running (re-dispatched), V re-armed.
	if _, err := goalSvc.SubmitVerdict(ctx, parseUUID(testWorkspaceID), subV, "reject", "missing tests"); err != nil {
		t.Fatalf("SubmitVerdict reject: %v", err)
	}
	taskV := drainGoalSubtaskTask(t, ctx, queries, subV)
	if _, err := taskSvc.CompleteTask(ctx, taskV.ID, []byte(`{"verdict":"reject"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask V: %v", err)
	}

	// A is re-running with a bumped attempt; B still pending (never unblocked).
	aAfter, _ := queries.GetGoalSubtask(ctx, subA)
	if aAfter.Status != "running" {
		t.Fatalf("reviewed node A: expected re-running, got %q", aAfter.Status)
	}
	if aAfter.Attempt <= aBefore.Attempt {
		t.Fatalf("reviewed node A: attempt should bump on reject (%d → %d)", aBefore.Attempt, aAfter.Attempt)
	}
	assertSubtaskStatus(t, ctx, queries, subV, "running") // re-armed + re-dispatched
	assertSubtaskStatus(t, ctx, queries, subB, "pending") // never unblocked under reject
}
