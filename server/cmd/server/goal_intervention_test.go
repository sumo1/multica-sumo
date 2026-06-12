package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// goalWithFailedRoot builds a confirmed goal [A, B(deps A)], drives A to a
// terminal failure with its attempt budget exhausted (so it escalated), leaving
// B blocked and the goal 'failed'. Returns the pieces for intervention tests.
func goalWithFailedRoot(t *testing.T, ctx context.Context) (
	*service.GoalService, *service.TaskService, *db.Queries,
	db.GoalRun, pgtype.UUID, pgtype.UUID,
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
		"Intervention goal", "A goal whose first node fails",
		[]service.SubtaskSpec{
			{Seq: 1, Title: "Step A", Spec: "do A", AssigneeAgentID: agentID},
			{Seq: 2, Title: "Step B", Spec: "do B", AssigneeAgentID: agentID, DependsOn: []int32{1}},
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

	// Exhaust A's retry budget so the failure is terminal (escalation).
	if _, err := testPool.Exec(ctx, `UPDATE goal_subtask SET max_attempts=1 WHERE id=$1`, subA); err != nil {
		t.Fatalf("set max_attempts: %v", err)
	}
	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.FailTask(ctx, taskA.ID, "boom", "", "", "agent_error"); err != nil {
		t.Fatalf("FailTask A: %v", err)
	}
	// A's failure now triggers a 下一步判断 (the engine asks the coordinator how to
	// proceed instead of blindly blocking). For the intervention fixtures we want
	// the classic "failed root + blocked downstream + goal failed" end-state, so
	// resolve the judgment with 'abort' — which reproduces exactly that.
	if _, err := goalSvc.DecideSubtask(ctx, parseUUID(testWorkspaceID), subA, "abort", ""); err != nil {
		t.Fatalf("DecideSubtask abort: %v", err)
	}
	// A failed, B blocked, goal failed (nothing completed).
	assertSubtaskStatus(t, ctx, queries, subA, "failed")
	assertSubtaskStatus(t, ctx, queries, subB, "blocked")
	if r, _ := queries.GetGoalRun(ctx, run.ID); r.Status != "failed" {
		t.Fatalf("expected goal 'failed', got %q", r.Status)
	}
	return goalSvc, taskSvc, queries, run, subA, subB
}

// TestGoalRetryRevivesGoal: retrying a failed node re-runs it with a fresh
// attempt budget and flips the goal back to 'executing'.
func TestGoalRetryRevivesGoal(t *testing.T) {
	ctx := context.Background()
	goalSvc, _, queries, run, subA, _ := goalWithFailedRoot(t, ctx)

	if _, err := goalSvc.RetrySubtask(ctx, parseUUID(testWorkspaceID), subA); err != nil {
		t.Fatalf("RetrySubtask: %v", err)
	}
	// A re-running with a reset attempt budget; goal back to executing.
	a, _ := queries.GetGoalSubtask(ctx, subA)
	if a.Status != "running" {
		t.Fatalf("retried node: expected 'running', got %q", a.Status)
	}
	if a.Attempt != 1 { // fresh rearm reset to 0, dispatch bumped to 1
		t.Fatalf("retried node: expected attempt=1 after fresh rearm+dispatch, got %d", a.Attempt)
	}
	r, _ := queries.GetGoalRun(ctx, run.ID)
	if r.Status != "executing" {
		t.Fatalf("goal should revive to 'executing', got %q", r.Status)
	}
}

// TestGoalSkipUnblocksDownstream: skipping a failed node marks it skipped and
// lets its blocked dependent become ready/dispatched.
func TestGoalSkipUnblocksDownstream(t *testing.T) {
	ctx := context.Background()
	goalSvc, _, queries, _, subA, subB := goalWithFailedRoot(t, ctx)

	if _, err := goalSvc.SkipSubtask(ctx, parseUUID(testWorkspaceID), subA); err != nil {
		t.Fatalf("SkipSubtask: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subA, "skipped")
	// B depended only on A; A skipped → B unblocks and dispatches.
	b, _ := queries.GetGoalSubtask(ctx, subB)
	if b.Status != "running" {
		t.Fatalf("downstream after skip: expected 'running', got %q", b.Status)
	}
}

// TestGoalReassignChangesAgentAndRevives: reassigning a failed node swaps the
// agent and re-runs it.
func TestGoalReassignChangesAgentAndRevives(t *testing.T) {
	ctx := context.Background()
	goalSvc, _, queries, _, subA, _ := goalWithFailedRoot(t, ctx)

	// Spin up a second agent in the fixture workspace (sharing the fixture
	// agent's online runtime) to reassign to.
	var runtimeID, newAgentID string
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id::text FROM agent WHERE workspace_id=$1 AND archived_at IS NULL ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&runtimeID); err != nil {
		t.Fatalf("load fixture runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'reassign-target-agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id::text
	`, parseUUID(testWorkspaceID), parseUUID(runtimeID), parseUUID(testUserID)).Scan(&newAgentID); err != nil {
		t.Fatalf("create reassign-target agent: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id=$1`, newAgentID) })

	if _, err := goalSvc.ReassignSubtask(ctx, parseUUID(testWorkspaceID), subA, parseUUID(newAgentID)); err != nil {
		t.Fatalf("ReassignSubtask: %v", err)
	}
	a, _ := queries.GetGoalSubtask(ctx, subA)
	if uuidToStringTest(a.AssigneeAgentID) != newAgentID {
		t.Fatalf("reassign: expected agent %s, got %s", newAgentID, uuidToStringTest(a.AssigneeAgentID))
	}
	if a.Status != "running" {
		t.Fatalf("reassigned node: expected 'running', got %q", a.Status)
	}
}

// TestGoalEditSpecChangesSpecAndRevives: editing the spec rewrites it and
// re-runs the node.
func TestGoalEditSpecChangesSpecAndRevives(t *testing.T) {
	ctx := context.Background()
	goalSvc, _, queries, _, subA, _ := goalWithFailedRoot(t, ctx)

	if _, err := goalSvc.EditSubtaskSpec(ctx, parseUUID(testWorkspaceID), subA, "do A but better"); err != nil {
		t.Fatalf("EditSubtaskSpec: %v", err)
	}
	a, _ := queries.GetGoalSubtask(ctx, subA)
	if a.Spec != "do A but better" {
		t.Fatalf("edit-spec: expected updated spec, got %q", a.Spec)
	}
	if a.Status != "running" {
		t.Fatalf("edited node: expected 'running', got %q", a.Status)
	}
}

func uuidToStringTest(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	const hex = "0123456789abcdef"
	buf := make([]byte, 36)
	pos := 0
	for i := 0; i < 16; i++ {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			buf[pos] = '-'
			pos++
		}
		buf[pos] = hex[b[i]>>4]
		buf[pos+1] = hex[b[i]&0x0f]
		pos += 2
	}
	return string(buf)
}

// TestGoalTakeoverCreatesChatSession: taking over a failed subtask creates a
// chat session bound to that subtask's agent, stamped with goal_subtask_id,
// without mutating the subtask/goal state.
func TestGoalTakeoverCreatesChatSession(t *testing.T) {
	ctx := context.Background()
	goalSvc, _, queries, _, subA, _ := goalWithFailedRoot(t, ctx)

	stBefore, _ := queries.GetGoalSubtask(ctx, subA)

	session, err := goalSvc.StartTakeover(ctx, parseUUID(testWorkspaceID), subA, parseUUID(testUserID))
	if err != nil {
		t.Fatalf("StartTakeover: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, session.ID)
	})

	// Session bound to the subtask's agent + stamped with goal_subtask_id.
	if uuidToStringTest(session.AgentID) != uuidToStringTest(stBefore.AssigneeAgentID) {
		t.Fatalf("takeover session agent %s != subtask agent %s",
			uuidToStringTest(session.AgentID), uuidToStringTest(stBefore.AssigneeAgentID))
	}
	if !session.GoalSubtaskID.Valid || uuidToStringTest(session.GoalSubtaskID) != uuidToStringTest(subA) {
		t.Fatalf("takeover session not stamped with the subtask id")
	}

	// Takeover must NOT change the subtask/goal — it only opens a conversation.
	stAfter, _ := queries.GetGoalSubtask(ctx, subA)
	if stAfter.Status != stBefore.Status {
		t.Fatalf("takeover should not change subtask status (%q → %q)", stBefore.Status, stAfter.Status)
	}
}
