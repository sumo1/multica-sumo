package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestTaskModeCreateDiscussionAndDynamicSquad verifies the task-mode entry:
// CreateTask resolves a PMO, builds a dynamic squad (leader=PMO + members),
// creates the goal in 'discussion', and opens a discussion chat bound to the
// PMO + linked to the goal.
func TestTaskModeCreateDiscussionAndDynamicSquad(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)

	// Fixture agent acts as both PMO (resolved via fallback) and member here.
	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id=$1 AND archived_at IS NULL AND runtime_id IS NOT NULL ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	run, chat, err := goalSvc.CreateTask(
		ctx, parseUUID(testWorkspaceID), parseUUID(testUserID),
		"Refactor login", "Refactor the login module",
		[]pgtype.UUID{parseUUID(agentID)}, pgtype.UUID{},
	)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id=$1`, run.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM squad WHERE id=$1`, run.SquadID)
	})

	// Goal is in discussion, on a freshly created dynamic squad.
	if run.Status != "discussion" {
		t.Fatalf("expected 'discussion', got %q", run.Status)
	}
	squad, err := queries.GetSquad(ctx, run.SquadID)
	if err != nil {
		t.Fatalf("GetSquad: %v", err)
	}
	if !strings.HasSuffix(squad.Name, "目标小队") {
		t.Fatalf("dynamic squad name should end in 目标小队, got %q", squad.Name)
	}
	// PMO is the squad leader (fallback resolved to the fixture agent).
	if uuidToStringTest(squad.LeaderID) != agentID {
		t.Fatalf("squad leader (PMO) should be the resolved agent")
	}

	// Discussion chat bound to PMO + linked to the goal.
	if uuidToStringTest(chat.AgentID) != agentID {
		t.Fatalf("discussion chat should bind the PMO agent")
	}
	if !chat.GoalRunID.Valid || uuidToStringTest(chat.GoalRunID) != uuidToStringTest(run.ID) {
		t.Fatalf("discussion chat should be linked to the goal_run")
	}
	if !run.ChatSessionID.Valid || uuidToStringTest(run.ChatSessionID) != uuidToStringTest(chat.ID) {
		t.Fatalf("goal_run should reference its discussion chat")
	}
}

// TestTaskModeConfirmDispatchesPlanning verifies the discussion → execution
// gate: ConfirmTask moves the goal to 'planning' and enqueues a planning task
// for the PMO (the dynamic squad's leader).
func TestTaskModeConfirmDispatchesPlanning(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id=$1 AND archived_at IS NULL AND runtime_id IS NOT NULL ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	run, _, err := goalSvc.CreateTask(
		ctx, parseUUID(testWorkspaceID), parseUUID(testUserID),
		"Ship feature", "Build feature X", nil, pgtype.UUID{},
	)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id=$1`, run.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM squad WHERE id=$1`, run.SquadID)
	})

	confirmed, err := goalSvc.ConfirmTask(ctx, parseUUID(testWorkspaceID), run.ID)
	if err != nil {
		t.Fatalf("ConfirmTask: %v", err)
	}
	if confirmed.Status != "planning" {
		t.Fatalf("expected 'planning' after confirm, got %q", confirmed.Status)
	}

	// A planning task was enqueued for the PMO (squad leader).
	var ptaskAgent string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id::text FROM agent_task_queue t JOIN agent a ON a.id=t.agent_id
		WHERE t.context::jsonb->>'goal_run_id' = $1
		  AND t.context::jsonb->>'type' = 'goal_planning'
		ORDER BY t.created_at DESC LIMIT 1
	`, run.ID).Scan(&ptaskAgent); err != nil {
		t.Fatalf("find planning task: %v", err)
	}
	squad, _ := queries.GetSquad(ctx, run.SquadID)
	if ptaskAgent != uuidToStringTest(squad.LeaderID) {
		t.Fatalf("planning task should go to the PMO (squad leader)")
	}
}

// TestTaskModeConfirmBackfillsGoalFromDiscussion verifies the conversational-
// create path: a task created with no goal text (the goal is described in chat)
// has its goal_run.goal backfilled from the user's discussion messages on
// confirm, so the PMO planning task receives a real goal to decompose.
func TestTaskModeConfirmBackfillsGoalFromDiscussion(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)

	// Empty title + goal — exactly what the + button creates now.
	run, chat, err := goalSvc.CreateTask(
		ctx, parseUUID(testWorkspaceID), parseUUID(testUserID),
		"", "", nil, pgtype.UUID{},
	)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id=$1`, run.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM squad WHERE id=$1`, run.SquadID)
	})
	if strings.TrimSpace(run.Goal) != "" {
		t.Fatalf("precondition: goal should start empty, got %q", run.Goal)
	}

	// The user describes the goal in the discussion (two messages); the PMO
	// replies in between — only the user's text should become the goal.
	insertChatMessage(ctx, t, chat.ID, "user", "给配置项起个好名字")
	insertChatMessage(ctx, t, chat.ID, "assistant", "好的，我先看看现有命名约定。")
	insertChatMessage(ctx, t, chat.ID, "user", "要兼容旧配置项")

	confirmed, err := goalSvc.ConfirmTask(ctx, parseUUID(testWorkspaceID), run.ID)
	if err != nil {
		t.Fatalf("ConfirmTask: %v", err)
	}
	if confirmed.Status != "planning" {
		t.Fatalf("expected 'planning' after confirm, got %q", confirmed.Status)
	}

	// goal_run.goal now holds the joined user messages (PMO reply excluded).
	reloaded, err := queries.GetGoalRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetGoalRun: %v", err)
	}
	if !strings.Contains(reloaded.Goal, "给配置项起个好名字") ||
		!strings.Contains(reloaded.Goal, "要兼容旧配置项") {
		t.Fatalf("goal should join the user messages, got %q", reloaded.Goal)
	}
	if strings.Contains(reloaded.Goal, "现有命名约定") {
		t.Fatalf("goal must not include the PMO's reply, got %q", reloaded.Goal)
	}
	if strings.TrimSpace(reloaded.Title) == "" {
		t.Fatalf("title should be derived from the goal when empty")
	}

	// The planning task carries the backfilled goal text.
	var planningGoal string
	if err := testPool.QueryRow(ctx, `
		SELECT t.context::jsonb->>'goal' FROM agent_task_queue t
		WHERE t.context::jsonb->>'goal_run_id' = $1
		  AND t.context::jsonb->>'type' = 'goal_planning'
		ORDER BY t.created_at DESC LIMIT 1
	`, run.ID).Scan(&planningGoal); err != nil {
		t.Fatalf("find planning task: %v", err)
	}
	if !strings.Contains(planningGoal, "给配置项起个好名字") {
		t.Fatalf("planning task should carry the backfilled goal, got %q", planningGoal)
	}
}

// TestTaskModeConfirmEmptyGoalErrors verifies confirm fails with a clear error
// when the goal is empty and the user has not described anything in the
// discussion — the only legitimate "no goal" case.
func TestTaskModeConfirmEmptyGoalErrors(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)

	run, _, err := goalSvc.CreateTask(
		ctx, parseUUID(testWorkspaceID), parseUUID(testUserID),
		"", "", nil, pgtype.UUID{},
	)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id=$1`, run.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM squad WHERE id=$1`, run.SquadID)
	})

	// No user messages → confirm must error, not dispatch an empty goal.
	_, err = goalSvc.ConfirmTask(ctx, parseUUID(testWorkspaceID), run.ID)
	if err == nil {
		t.Fatalf("expected error confirming a task with no goal")
	}
	if !strings.Contains(err.Error(), "no goal") {
		t.Fatalf("error should explain the missing goal, got %v", err)
	}

	// Goal stayed in discussion (not advanced to planning).
	reloaded, _ := queries.GetGoalRun(ctx, run.ID)
	if reloaded.Status != "discussion" {
		t.Fatalf("goal should stay in discussion on confirm failure, got %q", reloaded.Status)
	}
}

// insertChatMessage adds a chat_message row for a discussion test fixture.
func insertChatMessage(ctx context.Context, t *testing.T, sessionID pgtype.UUID, role, content string) {
	t.Helper()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, $2, $3)`,
		sessionID, role, content,
	); err != nil {
		t.Fatalf("insert chat message: %v", err)
	}
}

// TestTaskModeAddMember verifies a member can be added to the dynamic squad
// during discussion (PMO-suggested role).
func TestTaskModeAddMember(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)

	run, _, err := goalSvc.CreateTask(
		ctx, parseUUID(testWorkspaceID), parseUUID(testUserID),
		"Member test", "test", nil, pgtype.UUID{},
	)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id=$1`, run.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM squad WHERE id=$1`, run.SquadID)
	})

	// Create a second agent to add.
	var rtID, newAgent string
	_ = testPool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE workspace_id=$1 AND runtime_id IS NOT NULL LIMIT 1`, testWorkspaceID).Scan(&rtID)
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'task-member-agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3) RETURNING id::text
	`, parseUUID(testWorkspaceID), parseUUID(rtID), parseUUID(testUserID)).Scan(&newAgent); err != nil {
		t.Fatalf("create member agent: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id=$1`, newAgent) })

	if err := goalSvc.AddTaskMember(ctx, parseUUID(testWorkspaceID), run.ID, parseUUID(newAgent)); err != nil {
		t.Fatalf("AddTaskMember: %v", err)
	}
	members, _ := queries.ListSquadMembers(ctx, run.SquadID)
	found := false
	for _, m := range members {
		if uuidToStringTest(m.MemberID) == newAgent {
			found = true
		}
	}
	if !found {
		t.Fatalf("added agent should be a squad member")
	}
}
