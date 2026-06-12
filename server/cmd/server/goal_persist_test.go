package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestGoalPersistDispatchesSnapshotTask exercises the 持久化到工程 mechanism over
// the real HTTP router: a goal bound to a project with a local_directory repo →
// POST /persist enqueues a goal_persist task for the squad leader, carrying the
// harness slug + subtask digest + project resources (so the agent runs inside
// the repo). We verify the MECHANISM (per the "verify to mechanism, don't wait
// for the LLM" lesson) — not that the agent actually wrote files.
func TestGoalPersistDispatchesSnapshotTask(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	squadID, leaderID := goalTestSquad(t, ctx, queries)
	leaderStr := util.UUIDToString(leaderID)

	// A project with a local_directory resource → CanPersist precondition.
	proj, err := queries.CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Title:       "Persist Target",
		Status:      "planned",
		Priority:    "none",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id=$1`, proj.ID)
	})
	ref, _ := json.Marshal(map[string]string{"local_path": "/tmp/persist-repo", "daemon_id": "test"})
	if _, err := queries.CreateProjectResource(ctx, db.CreateProjectResourceParams{
		ProjectID:    proj.ID,
		WorkspaceID:  parseUUID(testWorkspaceID),
		ResourceType: "local_directory",
		ResourceRef:  ref,
		Position:     0,
		CreatedBy:    parseUUID(testUserID),
	}); err != nil {
		t.Fatalf("CreateProjectResource: %v", err)
	}

	// Create + plan a goal with two subtasks so the digest has content.
	var created struct {
		ID string `json:"id"`
	}
	planResp := authRequest(t, http.MethodPost, "/api/goals", map[string]any{
		"squad_id":  util.UUIDToString(squadID),
		"title":     "贪吃蛇 Evolution",
		"goal":      "Build a playable snake game",
		"confirmed": true,
		"subtasks": []map[string]any{
			{"seq": 1, "title": "Engine", "spec": "build the core loop", "assignee_agent_id": leaderStr, "depends_on": []int{}},
			{"seq": 2, "title": "UI", "spec": "render the board", "assignee_agent_id": leaderStr, "depends_on": []int{1}},
		},
	})
	readJSON(t, planResp, &created)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, created.ID)
	})

	// Bind the goal to the project (no create-path field for it in this test).
	if _, err := testPool.Exec(ctx, `UPDATE goal_run SET project_id=$1 WHERE id=$2`, proj.ID, created.ID); err != nil {
		t.Fatalf("bind project: %v", err)
	}

	// GET → CanPersist must be true now that a local repo is bound.
	getResp := authRequest(t, http.MethodGet, "/api/goals/"+created.ID, nil)
	var fetched struct {
		CanPersist    bool   `json:"can_persist"`
		PersistTaskID string `json:"persist_task_id"`
	}
	readJSON(t, getResp, &fetched)
	if !fetched.CanPersist {
		t.Fatal("can_persist should be true when a local_directory repo is bound")
	}
	if fetched.PersistTaskID != "" {
		t.Fatal("persist_task_id should be empty before any persist click")
	}

	// POST /persist → 202 with a dispatched persist task id.
	persistResp := authRequest(t, http.MethodPost, "/api/goals/"+created.ID+"/persist", nil)
	if persistResp.StatusCode != http.StatusAccepted {
		t.Fatalf("persist: expected 202, got %d", persistResp.StatusCode)
	}
	var dispatched struct {
		PersistTaskID string `json:"persist_task_id"`
	}
	readJSON(t, persistResp, &dispatched)
	if dispatched.PersistTaskID == "" {
		t.Fatal("persist response missing persist_task_id")
	}

	// The dispatched task must be a goal_persist task on the leader, with a
	// harness slug derived from the title and a digest carrying the subtasks.
	var ctxRaw []byte
	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT context, agent_id::text FROM agent_task_queue WHERE id=$1`, dispatched.PersistTaskID,
	).Scan(&ctxRaw, &agentID); err != nil {
		t.Fatalf("load persist task: %v", err)
	}
	if agentID != leaderStr {
		t.Fatalf("persist task should target the squad leader %s, got %s", leaderStr, agentID)
	}
	var pc service.GoalPersistContext
	if err := json.Unmarshal(ctxRaw, &pc); err != nil {
		t.Fatalf("unmarshal persist context: %v", err)
	}
	if pc.Type != service.GoalPersistContextType {
		t.Fatalf("expected type %q, got %q", service.GoalPersistContextType, pc.Type)
	}
	if pc.GoalRunID != created.ID {
		t.Fatalf("persist context goal_run_id mismatch: %q vs %q", pc.GoalRunID, created.ID)
	}
	if pc.ProjectID != util.UUIDToString(proj.ID) {
		t.Fatalf("persist context project_id mismatch")
	}
	// Slug = {YYMMDD}-{kebab}; CJK title kept, so the kebab segment is non-empty.
	if len(pc.Slug) < 8 || pc.Slug[6] != '-' {
		t.Fatalf("slug should be {YYMMDD}-{kebab}, got %q", pc.Slug)
	}
	if pc.SubtaskDigest == "" {
		t.Fatal("persist digest should carry the subtask content")
	}

	// GET again → persist_task_id is now surfaced.
	getResp2 := authRequest(t, http.MethodGet, "/api/goals/"+created.ID, nil)
	var refetched struct {
		PersistTaskID string `json:"persist_task_id"`
	}
	readJSON(t, getResp2, &refetched)
	if refetched.PersistTaskID != dispatched.PersistTaskID {
		t.Fatalf("persist_task_id should surface after persist: got %q want %q",
			refetched.PersistTaskID, dispatched.PersistTaskID)
	}

	// Snapshot is repeatable: a second persist re-uses the SAME slug (overwrite,
	// no new dated directory) — the slug is derived from created_at, not now.
	persist2 := authRequest(t, http.MethodPost, "/api/goals/"+created.ID+"/persist", nil)
	if persist2.StatusCode != http.StatusAccepted {
		t.Fatalf("second persist: expected 202, got %d", persist2.StatusCode)
	}
	var dispatched2 struct {
		PersistTaskID string `json:"persist_task_id"`
	}
	readJSON(t, persist2, &dispatched2)
	var ctxRaw2 []byte
	if err := testPool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE id=$1`, dispatched2.PersistTaskID).Scan(&ctxRaw2); err != nil {
		t.Fatalf("load second persist task: %v", err)
	}
	var pc2 service.GoalPersistContext
	_ = json.Unmarshal(ctxRaw2, &pc2)
	if pc2.Slug != pc.Slug {
		t.Fatalf("repeat persist must target the same slug (snapshot overwrite): %q vs %q", pc2.Slug, pc.Slug)
	}
}

// TestGoalPersistGatedWithoutLocalRepo locks the gating: a goal not bound to a
// project with a local repo cannot persist (button disabled / 400). This is the
// "平台自洽" guarantee — the platform runs fine without a repo; persist is an
// opt-in gain, never a precondition.
func TestGoalPersistGatedWithoutLocalRepo(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	squadID, leaderID := goalTestSquad(t, ctx, queries)
	leaderStr := util.UUIDToString(leaderID)

	var created struct {
		ID string `json:"id"`
	}
	planResp := authRequest(t, http.MethodPost, "/api/goals", map[string]any{
		"squad_id":  util.UUIDToString(squadID),
		"title":     "No repo task",
		"goal":      "runs entirely on the platform",
		"confirmed": true,
		"subtasks": []map[string]any{
			{"seq": 1, "title": "Step", "spec": "do", "assignee_agent_id": leaderStr, "depends_on": []int{}},
		},
	})
	readJSON(t, planResp, &created)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, created.ID)
	})

	// No project bound → CanPersist false.
	getResp := authRequest(t, http.MethodGet, "/api/goals/"+created.ID, nil)
	var fetched struct {
		CanPersist bool `json:"can_persist"`
	}
	readJSON(t, getResp, &fetched)
	if fetched.CanPersist {
		t.Fatal("can_persist must be false for a goal with no project repo")
	}

	// POST /persist → 400 (nowhere to write).
	persistResp := authRequest(t, http.MethodPost, "/api/goals/"+created.ID+"/persist", nil)
	if persistResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("persist without repo: expected 400, got %d", persistResp.StatusCode)
	}
	persistResp.Body.Close()
	_ = ctx
}
