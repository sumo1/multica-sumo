package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// TestClaimGoalPersistTaskCarriesRepoContext verifies the daemon claim mapping
// for a goal_persist task: the response must surface the persist run id + slug +
// digest AND the bound project's local_directory resource — because the daemon
// resolves the agent's work dir from ProjectResources, and without it the
// persist agent would have nowhere (no repo) to author the harness files.
//
// This is the mechanism check behind design test case #3 ("接力"): the agent runs
// inside the repo. We do NOT run the agent (that's LLM work) — we assert the
// claim hands it everything it needs.
func TestClaimGoalPersistTaskCarriesRepoContext(t *testing.T) {
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "Persist claim runtime")
	agentID, _ := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Persist claim agent")

	// A project with a local_directory resource (the repo the agent writes into).
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status, priority)
		VALUES ($1, 'Persist Claim Project', 'planned', 'none')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

	localRef, _ := json.Marshal(map[string]string{"local_path": "/tmp/persist-claim-repo", "daemon_id": "test"})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, position, created_by)
		VALUES ($1, $2, 'local_directory', $3, 0, $4)
	`, projectID, testWorkspaceID, localRef, testUserID); err != nil {
		t.Fatalf("create project resource: %v", err)
	}

	// A goal_persist task: FK-less, context JSONB carries everything (mirrors how
	// GoalService.PersistGoal enqueues it).
	persistCtx := service.GoalPersistContext{
		Type:          service.GoalPersistContextType,
		GoalRunID:     "11111111-1111-1111-1111-111111111111",
		WorkspaceID:   testWorkspaceID,
		ProjectID:     projectID,
		GoalTitle:     "贪吃蛇 Evolution",
		Goal:          "Build a playable snake game",
		Slug:          "260611-tan-chi-she-evolution",
		SubtaskDigest: "### [completed] Engine\nResult: core loop done\n",
		Outcome:       "completed",
	}
	ctxJSON, _ := json.Marshal(persistCtx)

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context)
		VALUES ($1, $2, 'queued', 5, $3)
		RETURNING id
	`, agentID, runtimeID, ctxJSON).Scan(&taskID); err != nil {
		t.Fatalf("create persist task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	// Claim it and decode the full task payload the daemon would receive.
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "persist-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("claim: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *struct {
			GoalPersistRunID string `json:"goal_persist_run_id"`
			GoalPersistSlug  string `json:"goal_persist_slug"`
			GoalPersistGoal  string `json:"goal_persist_goal"`
			GoalTitle        string `json:"goal_title"`
			ProjectID        string `json:"project_id"`
			ProjectResources []struct {
				ResourceType string `json:"resource_type"`
			} `json:"project_resources"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil {
		t.Fatalf("no task in claim response: %s", w.Body.String())
	}

	if resp.Task.GoalPersistRunID != persistCtx.GoalRunID {
		t.Errorf("goal_persist_run_id = %q, want %q", resp.Task.GoalPersistRunID, persistCtx.GoalRunID)
	}
	if resp.Task.GoalPersistSlug != persistCtx.Slug {
		t.Errorf("goal_persist_slug = %q, want %q", resp.Task.GoalPersistSlug, persistCtx.Slug)
	}
	if resp.Task.ProjectID != projectID {
		t.Errorf("project_id = %q, want %q", resp.Task.ProjectID, projectID)
	}
	// The crux: the agent must receive the local_directory so it runs in the repo.
	hasLocalDir := false
	for _, r := range resp.Task.ProjectResources {
		if r.ResourceType == "local_directory" {
			hasLocalDir = true
		}
	}
	if !hasLocalDir {
		t.Fatalf("persist claim must carry the project's local_directory resource (the agent's repo work dir), got %+v", resp.Task.ProjectResources)
	}
}

// makeRepoProject creates a project with a local_directory resource and returns
// its id. Shared by the planning/subtask repo-access tests.
func makeRepoProject(t *testing.T, ctx context.Context, title string) string {
	t.Helper()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status, priority)
		VALUES ($1, $2, 'planned', 'none') RETURNING id
	`, testWorkspaceID, title).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

	ref, _ := json.Marshal(map[string]string{"local_path": "/tmp/" + title, "daemon_id": "test"})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, position, created_by)
		VALUES ($1, $2, 'local_directory', $3, 0, $4)
	`, projectID, testWorkspaceID, ref, testUserID); err != nil {
		t.Fatalf("create project resource: %v", err)
	}
	return projectID
}

// claimAndCheckLocalDir claims the next task on the runtime and asserts the
// response carries the project's local_directory resource — the proof that the
// agent will run inside the repo and can read the project's contract dialect.
func claimAndCheckLocalDir(t *testing.T, runtimeID, projectID, label string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, label)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s claim: expected 200, got %d: %s", label, w.Code, w.Body.String())
	}
	var resp struct {
		Task *struct {
			ProjectID        string `json:"project_id"`
			ProjectResources []struct {
				ResourceType string `json:"resource_type"`
			} `json:"project_resources"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%s decode: %v", label, err)
	}
	if resp.Task == nil {
		t.Fatalf("%s: no task in claim response: %s", label, w.Body.String())
	}
	if resp.Task.ProjectID != projectID {
		t.Errorf("%s: project_id = %q, want %q", label, resp.Task.ProjectID, projectID)
	}
	hasLocalDir := false
	for _, r := range resp.Task.ProjectResources {
		if r.ResourceType == "local_directory" {
			hasLocalDir = true
		}
	}
	if !hasLocalDir {
		t.Fatalf("%s claim must carry local_directory so the agent reads the project's contract dialect, got %+v", label, resp.Task.ProjectResources)
	}
}

// TestClaimGoalPlanningTaskCarriesRepoContext verifies the planning agent runs
// inside the bound project's repo: a goal_planning task whose context carries a
// project_id must claim with the project's local_directory — otherwise the
// planner can't read the project's existing contracts to align its spec dialect.
func TestClaimGoalPlanningTaskCarriesRepoContext(t *testing.T) {
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Planning repo runtime")
	agentID, _ := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Planning repo agent")
	projectID := makeRepoProject(t, ctx, "planning-repo-proj")

	planCtx := service.GoalPlanningContext{
		Type:        service.GoalPlanningContextType,
		GoalRunID:   "22222222-2222-2222-2222-222222222222",
		WorkspaceID: testWorkspaceID,
		SquadID:     "33333333-3333-3333-3333-333333333333",
		GoalTitle:   "Ship onboarding",
		Goal:        "build it",
		ProjectID:   projectID,
	}
	ctxJSON, _ := json.Marshal(planCtx)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context)
		VALUES ($1, $2, 'queued', 5, $3) RETURNING id
	`, agentID, runtimeID, ctxJSON).Scan(&taskID); err != nil {
		t.Fatalf("create planning task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	claimAndCheckLocalDir(t, runtimeID, projectID, "planning")
}

// TestClaimGoalSubtaskTaskCarriesRepoContext verifies the executing agent runs
// inside the bound project's repo: a goal_subtask task whose context carries a
// project_id must claim with the project's local_directory, so the agent can
// follow the project's conventions and satisfy the spec's acceptance criteria.
func TestClaimGoalSubtaskTaskCarriesRepoContext(t *testing.T) {
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Subtask repo runtime")
	agentID, _ := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Subtask repo agent")
	projectID := makeRepoProject(t, ctx, "subtask-repo-proj")

	// A goal_subtask task needs a real goal_subtask row (FK). Build a minimal
	// squad + goal_run + subtask, then a task linking to it.
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'subtask-repo-squad', '', $2, $3) RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID) })

	var goalRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO goal_run (workspace_id, squad_id, creator_id, title, goal, status, project_id)
		VALUES ($1, $2, $3, 'g', 'g', 'executing', $4) RETURNING id
	`, testWorkspaceID, squadID, testUserID, projectID).Scan(&goalRunID); err != nil {
		t.Fatalf("create goal_run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM goal_run WHERE id = $1`, goalRunID) })

	var subtaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO goal_subtask (goal_run_id, seq, title, spec, status, kind)
		VALUES ($1, 1, 'Engine', 'build it', 'running', 'execute') RETURNING id
	`, goalRunID).Scan(&subtaskID); err != nil {
		t.Fatalf("create goal_subtask: %v", err)
	}

	subCtx := service.GoalSubtaskContext{
		Type:          service.GoalSubtaskContextType,
		GoalRunID:     goalRunID,
		GoalSubtaskID: subtaskID,
		WorkspaceID:   testWorkspaceID,
		GoalTitle:     "Ship onboarding",
		SubtaskTitle:  "Engine",
		Spec:          "build it",
		Kind:          "execute",
		ProjectID:     projectID,
	}
	ctxJSON, _ := json.Marshal(subCtx)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, goal_subtask_id, status, priority, context)
		VALUES ($1, $2, $3, 'queued', 5, $4) RETURNING id
	`, agentID, runtimeID, subtaskID, ctxJSON).Scan(&taskID); err != nil {
		t.Fatalf("create subtask task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	claimAndCheckLocalDir(t, runtimeID, projectID, "subtask")
}

// TestClaimDiscussionChatMarksGoalDiscussionActive verifies the discussion-phase
// facilitation hookup: a chat task on a session bound to a goal_run in
// 'discussion' status must claim with goal_discussion_active=true (so the chat
// prompt frames the agent as the 总控 facilitator). Once the goal leaves
// 'discussion', the flag must NOT be set — the chat reverts to a normal one.
func TestClaimDiscussionChatMarksGoalDiscussionActive(t *testing.T) {
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Discussion chat runtime")
	agentID, _ := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Discussion chat agent")

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'discussion-squad', '', $2, $3) RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID) })

	// A goal_run in 'discussion' + its discussion chat session.
	var goalRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO goal_run (workspace_id, squad_id, creator_id, title, goal, status)
		VALUES ($1, $2, $3, 'Dark mode', 'add a dark theme', 'discussion') RETURNING id
	`, testWorkspaceID, squadID, testUserID).Scan(&goalRunID); err != nil {
		t.Fatalf("create goal_run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM goal_run WHERE id = $1`, goalRunID) })

	var chatID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id, goal_run_id)
		VALUES ($1, $2, $3, 'discussion', $4, $5) RETURNING id
	`, testWorkspaceID, agentID, testUserID, runtimeID, goalRunID).Scan(&chatID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, chatID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'I want dark mode')
	`, chatID); err != nil {
		t.Fatalf("create chat message: %v", err)
	}

	mkChatTask := func() string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority)
			VALUES ($1, $2, $3, 'queued', 5) RETURNING id
		`, agentID, runtimeID, chatID).Scan(&id); err != nil {
			t.Fatalf("create chat task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, id) })
		return id
	}

	claimDiscussionActive := func(label string) bool {
		mkChatTask()
		w := httptest.NewRecorder()
		req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
			testWorkspaceID, label)
		req = withURLParam(req, "runtimeId", runtimeID)
		testHandler.ClaimTaskByRuntime(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s claim: expected 200, got %d: %s", label, w.Code, w.Body.String())
		}
		var resp struct {
			Task *struct {
				GoalDiscussionActive bool `json:"goal_discussion_active"`
			} `json:"task"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s decode: %v", label, err)
		}
		if resp.Task == nil {
			t.Fatalf("%s: no task: %s", label, w.Body.String())
		}
		return resp.Task.GoalDiscussionActive
	}

	// Allow back-to-back claims (default fixture agent caps at 1 concurrent).
	if _, err := testPool.Exec(ctx, `UPDATE agent SET max_concurrent_tasks=10 WHERE id=$1`, agentID); err != nil {
		t.Fatalf("bump concurrency: %v", err)
	}
	// clearInFlight completes any non-terminal task so the next claim isn't
	// blocked by the previous one still occupying the agent.
	clearInFlight := func() {
		testPool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE agent_id=$1 AND status NOT IN ('completed','failed','cancelled')`, agentID)
	}

	// While 'discussion' → flag set.
	if !claimDiscussionActive("discussion") {
		t.Fatal("chat on a discussion-phase goal must claim with goal_discussion_active=true")
	}
	clearInFlight()

	// After the goal leaves discussion → flag clears.
	if _, err := testPool.Exec(ctx, `UPDATE goal_run SET status='executing' WHERE id=$1`, goalRunID); err != nil {
		t.Fatalf("advance goal: %v", err)
	}
	if claimDiscussionActive("executing") {
		t.Fatal("once the goal leaves 'discussion', the chat must NOT be marked goal_discussion_active")
	}
}
