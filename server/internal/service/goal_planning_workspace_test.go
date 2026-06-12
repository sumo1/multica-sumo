package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestGoalPlanningWorkspaceID pins the workspace resolution for goal-planning
// tasks, which carry no FK column — the workspace lives only in the context
// JSONB. ReportTaskMessages / broadcastTaskEvent rely on this returning the
// workspace so the task:message WS stream actually broadcasts; when it returned
// "" the PMO main session in the Task page ④ column stayed empty (messages were
// persisted but never pushed live). Found via real-machine inspection
// 2026-06-10.
func TestGoalPlanningWorkspaceID(t *testing.T) {
	const ws = "f78367c6-b61e-4ff5-ae1f-2110dabbeeff"

	planning := db.AgentTaskQueue{
		Context: []byte(`{"type":"goal_planning","goal_run_id":"180120d1-1498-423b-b8fe-ca7e880e64d5","workspace_id":"` + ws + `","squad_id":"s","goal_title":"t","goal":"g"}`),
	}
	if got := goalContextWorkspaceID(planning); got != ws {
		t.Fatalf("goal-planning task: expected workspace %q, got %q", ws, got)
	}

	// A summary task (FK-less, type=goal_summary) must also resolve — otherwise
	// the PMO 收口 stream's task:message broadcasts drop.
	summary := db.AgentTaskQueue{
		Context: []byte(`{"type":"goal_summary","goal_run_id":"180120d1-1498-423b-b8fe-ca7e880e64d5","workspace_id":"` + ws + `","outcome":"completed"}`),
	}
	if got := goalContextWorkspaceID(summary); got != ws {
		t.Fatalf("goal-summary task: expected workspace %q, got %q", ws, got)
	}

	// A non-goal context must not be mistaken for one.
	quick := db.AgentTaskQueue{
		Context: []byte(`{"type":"quick_create","workspace_id":"` + ws + `"}`),
	}
	if got := goalContextWorkspaceID(quick); got != "" {
		t.Fatalf("non-goal task: expected \"\", got %q", got)
	}

	// A task with an FK link is never a planning task (planning tasks have no FK).
	withChat := db.AgentTaskQueue{
		ChatSessionID: pgtype.UUID{Valid: true},
		Context:       planning.Context,
	}
	if got := goalContextWorkspaceID(withChat); got != "" {
		t.Fatalf("FK-linked task: expected \"\", got %q", got)
	}

	// Empty / malformed context → "".
	if got := goalContextWorkspaceID(db.AgentTaskQueue{}); got != "" {
		t.Fatalf("empty context: expected \"\", got %q", got)
	}
}
