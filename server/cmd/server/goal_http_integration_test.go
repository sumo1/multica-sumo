package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestGoalHTTPAutoDecomposeFlow exercises the full LLM-decomposition flow over
// the real HTTP router: create a goal with auto_decompose → it lands in
// 'planning' with a dispatched planning task → the leader's plan is submitted
// via the plan endpoint → the goal flips to 'executing' with the subtask DAG.
// This is the end-to-end seam test (routes + handlers + service + DB), beyond
// the service-level unit tests.
func TestGoalHTTPAutoDecomposeFlow(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	// A squad led by the fixture agent (online cloud runtime → dispatch passes).
	squadID, leaderID := goalTestSquad(t, ctx, queries)
	leaderStr := util.UUIDToString(leaderID)

	// 1. Create with auto_decompose → planning task dispatched to the leader.
	createResp := authRequest(t, http.MethodPost, "/api/goals", map[string]any{
		"squad_id":       util.UUIDToString(squadID),
		"title":          "Ship onboarding",
		"goal":           "Build the onboarding flow end to end",
		"auto_decompose": true,
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create goal: expected 201, got %d", createResp.StatusCode)
	}
	var created struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Subtasks []struct {
			ID string `json:"id"`
		} `json:"subtasks"`
	}
	readJSON(t, createResp, &created)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, created.ID)
	})

	if created.Status != "planning" {
		t.Fatalf("expected status 'planning', got %q", created.Status)
	}
	if len(created.Subtasks) != 0 {
		t.Fatalf("planning goal should have no subtasks yet, got %d", len(created.Subtasks))
	}

	// auto_decompose + explicit subtasks must be rejected (mutually exclusive).
	badResp := authRequest(t, http.MethodPost, "/api/goals", map[string]any{
		"squad_id":       util.UUIDToString(squadID),
		"auto_decompose": true,
		"subtasks":       []map[string]any{{"seq": 1, "title": "x", "spec": "y"}},
	})
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for auto_decompose+subtasks, got %d", badResp.StatusCode)
	}
	badResp.Body.Close()

	// 2. Leader submits the plan (what `multica goal plan` POSTs).
	planResp := authRequest(t, http.MethodPost, "/api/goals/"+created.ID+"/plan", map[string]any{
		"subtasks": []map[string]any{
			{"seq": 1, "title": "Backend", "spec": "build API", "assignee_agent_id": leaderStr, "depends_on": []int{}},
			{"seq": 2, "title": "Frontend", "spec": "build UI", "assignee_agent_id": leaderStr, "depends_on": []int{1}},
		},
	})
	if planResp.StatusCode != http.StatusOK {
		t.Fatalf("submit plan: expected 200, got %d", planResp.StatusCode)
	}
	var planned struct {
		Status   string `json:"status"`
		Subtasks []struct {
			Seq             int      `json:"seq"`
			Status          string   `json:"status"`
			AssigneeAgentID string   `json:"assignee_agent_id"`
			DependsOn       []string `json:"depends_on"`
		} `json:"subtasks"`
	}
	readJSON(t, planResp, &planned)

	if planned.Status != "executing" {
		t.Fatalf("expected status 'executing' after plan, got %q", planned.Status)
	}
	if len(planned.Subtasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(planned.Subtasks))
	}

	// Root (seq 1) dispatched → running; dependent (seq 2) pending with a dep.
	var root, dependent *int
	for i := range planned.Subtasks {
		st := planned.Subtasks[i]
		if st.Seq == 1 {
			root = &i
		}
		if st.Seq == 2 {
			dependent = &i
		}
	}
	if root == nil || dependent == nil {
		t.Fatal("missing expected subtasks by seq")
	}
	if planned.Subtasks[*root].Status != "running" {
		t.Fatalf("root subtask: expected 'running', got %q", planned.Subtasks[*root].Status)
	}
	if planned.Subtasks[*dependent].Status != "pending" {
		t.Fatalf("dependent subtask: expected 'pending', got %q", planned.Subtasks[*dependent].Status)
	}
	if len(planned.Subtasks[*dependent].DependsOn) != 1 {
		t.Fatalf("dependent subtask should declare 1 dependency, got %d", len(planned.Subtasks[*dependent].DependsOn))
	}

	// 3. GET round-trips the same executing goal with its DAG.
	getResp := authRequest(t, http.MethodGet, "/api/goals/"+created.ID, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get goal: expected 200, got %d", getResp.StatusCode)
	}
	var fetched struct {
		Status   string `json:"status"`
		Subtasks []any  `json:"subtasks"`
	}
	readJSON(t, getResp, &fetched)
	if fetched.Status != "executing" || len(fetched.Subtasks) != 2 {
		t.Fatalf("get goal mismatch: status=%q subtasks=%d", fetched.Status, len(fetched.Subtasks))
	}
}

// TestGoalResponseExposesResultAndTaskIDs locks in the fix for "I can't see the
// execution content": GET /api/goals/{id} must expose each subtask's result +
// execution task_id, and the goal's planning_task_id, so the UI ④ column can
// fetch the task_messages stream and show output. Found via real-machine UI
// inspection 2026-06-10 (results existed in DB but were dropped by the API).
func TestGoalResponseExposesResultAndTaskIDs(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	squadID, leaderID := goalTestSquad(t, ctx, queries)
	leaderStr := util.UUIDToString(leaderID)

	// Create + plan a goal with one execute subtask, then drive it to completed
	// with a result, recording a task_message so the stream has content.
	createResp := authRequest(t, http.MethodPost, "/api/goals", map[string]any{
		"squad_id": util.UUIDToString(squadID),
		"title":    "Result exposure",
		"goal":     "verify result+task_id surface",
	})
	createResp.Body.Close()
	// Use the service-style plan submit endpoint.
	var created struct {
		ID string `json:"id"`
	}
	planFor := authRequest(t, http.MethodPost, "/api/goals", map[string]any{
		"squad_id":  util.UUIDToString(squadID),
		"title":     "Result exposure 2",
		"goal":      "g",
		"confirmed": true,
		"subtasks": []map[string]any{
			{"seq": 1, "title": "Do it", "spec": "do", "assignee_agent_id": leaderStr, "depends_on": []int{}},
		},
	})
	readJSON(t, planFor, &created)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, created.ID)
	})

	// Find the dispatched execution task for the subtask, complete it with a result.
	var subID, taskID string
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM goal_subtask WHERE goal_run_id=$1 LIMIT 1`, created.ID).Scan(&subID); err != nil {
		t.Fatalf("subtask: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM agent_task_queue WHERE goal_subtask_id=$1 ORDER BY created_at DESC LIMIT 1`, subID).Scan(&taskID); err != nil {
		t.Fatalf("exec task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status='completed', result='{"output":"the answer"}'::jsonb WHERE id=$1`, taskID); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE goal_subtask SET status='completed', result='{"output":"the answer"}'::jsonb WHERE id=$1`, subID); err != nil {
		t.Fatalf("complete subtask: %v", err)
	}

	// GET the goal → response must carry result + task_id on the subtask.
	getResp := authRequest(t, http.MethodGet, "/api/goals/"+created.ID, nil)
	var fetched struct {
		Subtasks []struct {
			TaskID string          `json:"task_id"`
			Result json.RawMessage `json:"result"`
		} `json:"subtasks"`
	}
	readJSON(t, getResp, &fetched)
	if len(fetched.Subtasks) == 0 {
		t.Fatal("no subtasks in response")
	}
	st := fetched.Subtasks[0]
	if st.TaskID == "" {
		t.Fatal("subtask response missing task_id (UI can't fetch the stream)")
	}
	if len(st.Result) == 0 || !bytes.Contains(st.Result, []byte("the answer")) {
		t.Fatalf("subtask response missing result content, got %q", string(st.Result))
	}
}

// TestGoalResponseExposesAttribution locks in the multi-runtime observability
// ask: GET /api/goals/{id} must say WHICH agent / runtime / model ran each
// subtask and the coordinator. Model follows "method 3": the agent's configured
// model while running, upgraded to the actually-used model (from task_usage)
// once the task reports usage.
func TestGoalResponseExposesAttribution(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	squadID, leaderID := goalTestSquad(t, ctx, queries)
	leaderStr := util.UUIDToString(leaderID)

	// Give the assignee/leader agent a configured model so we can assert it.
	if _, err := testPool.Exec(ctx, `UPDATE agent SET model='claude-opus-4-8' WHERE id=$1`, leaderStr); err != nil {
		t.Fatalf("set agent model: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `UPDATE agent SET model=NULL WHERE id=$1`, leaderStr)
	})

	var created struct {
		ID string `json:"id"`
	}
	planResp := authRequest(t, http.MethodPost, "/api/goals", map[string]any{
		"squad_id":  util.UUIDToString(squadID),
		"title":     "Attribution check",
		"goal":      "g",
		"confirmed": true,
		"subtasks": []map[string]any{
			{"seq": 1, "title": "Do it", "spec": "do", "assignee_agent_id": leaderStr, "depends_on": []int{}},
		},
	})
	readJSON(t, planResp, &created)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, created.ID)
	})

	var subID, taskID string
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM goal_subtask WHERE goal_run_id=$1 LIMIT 1`, created.ID).Scan(&subID); err != nil {
		t.Fatalf("subtask: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM agent_task_queue WHERE goal_subtask_id=$1 ORDER BY created_at DESC LIMIT 1`, subID).Scan(&taskID); err != nil {
		t.Fatalf("exec task: %v", err)
	}

	type attrSubtask struct {
		AgentName       string `json:"agent_name"`
		RuntimeName     string `json:"runtime_name"`
		RuntimeProvider string `json:"runtime_provider"`
		Model           string `json:"model"`
	}
	type attrRun struct {
		CoordinatorName    string        `json:"coordinator_name"`
		CoordinatorModel   string        `json:"coordinator_model"`
		CoordinatorRuntime string        `json:"coordinator_runtime_name"`
		Subtasks           []attrSubtask `json:"subtasks"`
	}

	// Before usage is reported → model is the agent's CONFIGURED model.
	var before attrRun
	readJSON(t, authRequest(t, http.MethodGet, "/api/goals/"+created.ID, nil), &before)
	if len(before.Subtasks) == 0 {
		t.Fatal("no subtasks in response")
	}
	st := before.Subtasks[0]
	if st.AgentName == "" {
		t.Fatal("subtask attribution missing agent_name")
	}
	if st.RuntimeName == "" || st.RuntimeProvider == "" {
		t.Fatalf("subtask attribution missing runtime (name=%q provider=%q)", st.RuntimeName, st.RuntimeProvider)
	}
	if st.Model != "claude-opus-4-8" {
		t.Fatalf("pre-usage model should be the configured model, got %q", st.Model)
	}
	if before.CoordinatorName == "" || before.CoordinatorModel != "claude-opus-4-8" {
		t.Fatalf("coordinator attribution wrong: name=%q model=%q", before.CoordinatorName, before.CoordinatorModel)
	}

	// Report actual usage with a DIFFERENT model → response must upgrade to it.
	if _, err := testPool.Exec(ctx,
		`INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens)
		 VALUES ($1, 'bedrock', 'claude-sonnet-4-6-actual', 10, 20, 0, 0)`, taskID); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}

	var after attrRun
	readJSON(t, authRequest(t, http.MethodGet, "/api/goals/"+created.ID, nil), &after)
	if after.Subtasks[0].Model != "claude-sonnet-4-6-actual" {
		t.Fatalf("post-usage model should upgrade to the actually-used model, got %q", after.Subtasks[0].Model)
	}
}

// TestReportTaskMessagesBroadcastsForGoalTask locks the fix for "I can't see the
// PMO / subtask execution stream": ReportTaskMessages must resolve the workspace
// via the shared resolver (handles goal_subtask / goal_planning), otherwise the
// task:message WS broadcast is dropped and the Task page ④ column never fills
// live (taskMessagesOptions uses staleTime: Infinity and relies on the WS push).
// Found via real-machine inspection 2026-06-10 — messages were persisted but the
// stream stayed empty during and right after execution.
func TestReportTaskMessagesBroadcastsForGoalTask(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	squadID, leaderID := goalTestSquad(t, ctx, queries)
	leaderStr := util.UUIDToString(leaderID)

	var created struct {
		ID string `json:"id"`
	}
	planResp := authRequest(t, http.MethodPost, "/api/goals", map[string]any{
		"squad_id":  util.UUIDToString(squadID),
		"title":     "Broadcast check",
		"goal":      "g",
		"confirmed": true,
		"subtasks": []map[string]any{
			{"seq": 1, "title": "Do it", "spec": "do", "assignee_agent_id": leaderStr, "depends_on": []int{}},
		},
	})
	readJSON(t, planResp, &created)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, created.ID)
	})

	var subID, taskID string
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM goal_subtask WHERE goal_run_id=$1 LIMIT 1`, created.ID).Scan(&subID); err != nil {
		t.Fatalf("subtask: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM agent_task_queue WHERE goal_subtask_id=$1 ORDER BY created_at DESC LIMIT 1`, subID).Scan(&taskID); err != nil {
		t.Fatalf("exec task: %v", err)
	}

	// Subscribe to task:message before posting; capture broadcasts for our task.
	var (
		mu   sync.Mutex
		seen []string
	)
	testBus.Subscribe("task:message", func(e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.TaskID == taskID || e.WorkspaceID == testWorkspaceID {
			seen = append(seen, e.TaskID)
		}
	})

	// Daemon reports a message for the goal subtask's exec task.
	resp := authRequest(t, http.MethodPost, "/api/daemon/tasks/"+taskID+"/messages", map[string]any{
		"messages": []map[string]any{
			{"seq": 1, "type": "text", "content": "hello from the agent"},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("report messages: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// The bus is synchronous, so the broadcast has already fired by now.
	mu.Lock()
	got := len(seen)
	mu.Unlock()
	if got == 0 {
		t.Fatal("ReportTaskMessages did not broadcast task:message for a goal subtask task (workspace resolution dropped it)")
	}
}
