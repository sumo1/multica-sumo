package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Task mode (design-task-mode.md): PMO planning layer + dynamic squad.
// Distinct from the lower-level /api/goals endpoints (which the thin-slice /
// explicit-plan path uses). Task mode is the user-facing flow: create (discussion)
// → discuss with PMO → confirm → execute.
// ---------------------------------------------------------------------------

// CreateTaskRequest starts a task in the discussion phase. members is the set
// of agent ids the user picked (may be empty; PMO can pick during planning or
// the user adds during discussion).
type CreateTaskRequest struct {
	Title   string   `json:"title"`
	Goal    string   `json:"goal"`
	Members []string `json:"members"`
	// ProjectID optionally binds the task to a dependency project (its repo is
	// the role-sync source and the PMO's planning context). Empty = no project.
	ProjectID string `json:"project_id"`
}

// TaskResponse bundles the goal run + its discussion chat session id so the UI
// can immediately open the discussion conversation.
type TaskResponse struct {
	Goal             GoalRunResponse `json:"goal"`
	DiscussionChatID string          `json:"discussion_chat_id"`
}

// ListTasks returns the workspace's tasks (goals), newest first, each with its
// subtasks so the list can show progress.
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	runs, err := h.Queries.ListGoalRunsForWorkspace(r.Context(), db.ListGoalRunsForWorkspaceParams{
		WorkspaceID: workspaceUUID,
		Limit:       100,
		Offset:      0,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}
	out := make([]GoalRunResponse, 0, len(runs))
	for _, run := range runs {
		subtasks, _ := h.Queries.ListGoalSubtasks(r.Context(), run.ID)
		resp := goalRunToResponse(run, subtasks)
		h.enrichGoalResponse(r.Context(), &resp, run)
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	members := make([]pgtype.UUID, 0, len(req.Members))
	for _, m := range req.Members {
		parsed, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(m), "member agent id")
		if !ok {
			return
		}
		members = append(members, parsed)
	}

	// project_id is optional; validate only when present.
	var projectID pgtype.UUID
	if pid := strings.TrimSpace(req.ProjectID); pid != "" {
		parsed, ok := parseUUIDOrBadRequest(w, pid, "project_id")
		if !ok {
			return
		}
		projectID = parsed
	}

	run, chat, err := h.GoalService.CreateTask(
		r.Context(), workspaceUUID, parseUUID(userID),
		req.Title, req.Goal, members, projectID,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	subtasks, _ := h.Queries.ListGoalSubtasks(r.Context(), run.ID)
	writeJSON(w, http.StatusCreated, TaskResponse{
		Goal:             goalRunToResponse(run, subtasks),
		DiscussionChatID: util.UUIDToString(chat.ID),
	})
}

// AddTaskMemberRequest adds an agent to the task's dynamic squad.
type AddTaskMemberRequest struct {
	AgentID string `json:"agent_id"`
}

func (h *Handler) AddTaskMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	goalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "task id")
	if !ok {
		return
	}
	var req AddTaskMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.AgentID), "agent_id")
	if !ok {
		return
	}
	if err := h.GoalService.AddTaskMember(r.Context(), workspaceUUID, goalID, agentID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.writeTaskGoal(w, r, goalID, workspaceUUID)
}

// ConfirmTask passes the discussion → execution gate (PMO decomposes the goal).
func (h *Handler) ConfirmTask(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	goalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "task id")
	if !ok {
		return
	}
	if _, err := h.GoalService.ConfirmTask(r.Context(), workspaceUUID, goalID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.writeTaskGoal(w, r, goalID, workspaceUUID)
}

// SetPlannerRequest sets the workspace's default PMO/planner agent. Empty
// agent_id clears it (falls back to the first available agent).
type SetPlannerRequest struct {
	AgentID string `json:"agent_id"`
}

func (h *Handler) SetWorkspacePlanner(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	var req SetPlannerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var plannerID pgtype.UUID
	if strings.TrimSpace(req.AgentID) != "" {
		parsed, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.AgentID), "agent_id")
		if !ok {
			return
		}
		// Validate the agent belongs to the workspace.
		agent, err := h.Queries.GetAgent(r.Context(), parsed)
		if err != nil || agent.WorkspaceID != workspaceUUID {
			writeError(w, http.StatusBadRequest, "agent not found in workspace")
			return
		}
		plannerID = parsed
	}
	if _, err := h.Queries.SetWorkspaceDefaultPlanner(r.Context(), db.SetWorkspaceDefaultPlannerParams{
		ID:                    workspaceUUID,
		DefaultPlannerAgentID: plannerID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set planner")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SyncProjectRoles reads role definitions from a project's bound local directory
// and materializes them as workspace Agents (the "associate a project → auto-sync
// its roles" capability). Returns the created/updated/skipped role names.
func (h *Handler) SyncProjectRoles(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project_id")
	if !ok {
		return
	}

	// Ensure the project belongs to the workspace before scanning its directory.
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID:          projectUUID,
		WorkspaceID: workspaceUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	result, err := h.RoleSyncService.SyncProjectRoles(
		r.Context(), workspaceUUID, projectUUID, parseUUID(userID),
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// writeTaskGoal returns the goal + subtasks for a task-mode goal.
func (h *Handler) writeTaskGoal(w http.ResponseWriter, r *http.Request, goalID, workspaceUUID pgtype.UUID) {
	run, err := h.Queries.GetGoalRunInWorkspace(r.Context(), db.GetGoalRunInWorkspaceParams{
		ID:          goalID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	subtasks, err := h.Queries.ListGoalSubtasks(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load subtasks")
		return
	}
	resp := goalRunToResponse(run, subtasks)
	h.enrichGoalResponse(r.Context(), &resp, run)
	writeJSON(w, http.StatusOK, resp)
}
