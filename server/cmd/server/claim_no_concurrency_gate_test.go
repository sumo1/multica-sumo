package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestClaimDispatchesPastConcurrencyLimit locks the decision to NOT proactively
// queue: a per-agent max_concurrent_tasks=1 must NOT hold a second task in
// 'queued'. Both tasks claim immediately; we dispatch straight to the agent and
// let the runtime's own error surface if it genuinely can't cope. (Before this
// change, ClaimTask returned nil for the second task — "no capacity" — leaving
// it queued until a slot freed.)
func TestClaimDispatchesPastConcurrencyLimit(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	// Use the workspace fixture agent (it has an online cloud runtime) and pin
	// its concurrency limit to 1 — the most restrictive case.
	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text, runtime_id::text FROM agent
		   WHERE workspace_id=$1 AND runtime_id IS NOT NULL
		   ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET max_concurrent_tasks=1 WHERE id=$1`, agentID); err != nil {
		t.Fatalf("pin concurrency: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `UPDATE agent SET max_concurrent_tasks=6 WHERE id=$1`, agentID)
	})

	// Enqueue two tasks on DIFFERENT issues for the same agent. Distinct
	// issue_id means they do NOT share the per-issue serialization key — so the
	// ONLY thing that could have blocked the second claim was the per-agent
	// concurrency gate we removed.
	issue1 := createTestIssue(t, testWorkspaceID, testUserID)
	issue2 := createTestIssue(t, testWorkspaceID, testUserID)
	mkTask := func(issueID string) {
		if _, err := queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
			AgentID:   parseUUID(agentID),
			RuntimeID: parseUUID(runtimeID),
			IssueID:   parseUUID(issueID),
			Priority:  5,
		}); err != nil {
			t.Fatalf("enqueue task: %v", err)
		}
	}
	mkTask(issue1)
	mkTask(issue2)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE agent_id=$1 AND issue_id IN ($2,$3)`, agentID, issue1, issue2)
	})

	// First claim → a task. With max_concurrent=1 it is now "running" (dispatched).
	first, err := taskSvc.ClaimTask(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if first == nil {
		t.Fatal("first claim returned no task")
	}

	// Second claim → MUST still return a task even though the agent already has
	// one dispatched. The old behavior returned nil here (held in queue).
	second, err := taskSvc.ClaimTask(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if second == nil {
		t.Fatal("second claim returned nil — a concurrency gate is still holding the task in 'queued' (we removed proactive queueing)")
	}
	if util.UUIDToString(first.ID) == util.UUIDToString(second.ID) {
		t.Fatal("second claim returned the SAME task as the first")
	}
}
