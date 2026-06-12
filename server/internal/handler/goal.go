package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Goals (PMO orchestration)
// ---------------------------------------------------------------------------

// CreateGoalRequest creates a goal on a squad. Two paths:
//   - Explicit: caller passes a subtask list (+ confirmed=true) to run now.
//   - Auto-decompose: auto_decompose=true dispatches a planning task to the
//     squad leader (PMO), who decomposes via LLM and submits the plan, which
//     produces the same subtask list through SubmitPlan.
type CreateGoalRequest struct {
	SquadID       string               `json:"squad_id"`
	ChatSessionID *string              `json:"chat_session_id"`
	Title         string               `json:"title"`
	Goal          string               `json:"goal"`
	Confirmed     bool                 `json:"confirmed"`
	Subtasks      []CreateSubtaskInput `json:"subtasks"`
	// AutoDecompose dispatches a planning task to the squad leader instead of
	// accepting an explicit subtask list. Mutually exclusive with Subtasks.
	AutoDecompose bool `json:"auto_decompose"`
}

// CreateSubtaskInput is one node of the decomposition. DependsOn references the
// Seq values of upstream subtasks in this same request (seq-relative DAG).
type CreateSubtaskInput struct {
	Seq             int32   `json:"seq"`
	Title           string  `json:"title"`
	Spec            string  `json:"spec"`
	AssigneeAgentID *string `json:"assignee_agent_id"`
	DependsOn       []int32 `json:"depends_on"`
	// Kind is "execute" (default) or "verify". Verify nodes adversarially
	// review the output of the nodes they depend on.
	Kind string `json:"kind"`
}

// GoalRunResponse is the API shape for a goal_run plus its subtasks.
type GoalRunResponse struct {
	ID            string                `json:"id"`
	WorkspaceID   string                `json:"workspace_id"`
	SquadID       string                `json:"squad_id"`
	ChatSessionID string                `json:"chat_session_id,omitempty"`
	Title         string                `json:"title"`
	Goal          string                `json:"goal"`
	Status        string                `json:"status"`
	Subtasks      []GoalSubtaskResponse `json:"subtasks"`
	// PlanningTaskID is the PMO planning task — its task_messages are the main
	// session execution stream (④ column). Empty before planning is dispatched.
	PlanningTaskID string `json:"planning_task_id,omitempty"`
	// SummaryTaskID is the PMO 收口/汇总 task — its task_messages are the final
	// deliverable, shown as the tail of the main session (④). Empty until all
	// subtasks finish and the summary is dispatched.
	SummaryTaskID string `json:"summary_task_id,omitempty"`
	// PersistTaskID is the most recent repo-persist (one-click snapshot) task.
	// Surfaced so the UI can show the persist stream / "已持久化" affordance.
	// Empty until the user clicks 持久化到工程 at least once.
	PersistTaskID string `json:"persist_task_id,omitempty"`
	// CanPersist is true when the task is bound to a project carrying a local
	// repo (local_directory) — the precondition for the 持久化到工程 button. The
	// frontend uses this to enable/disable the button without a second call.
	CanPersist bool `json:"can_persist"`
	// ConfirmedAt is the discussion → execution gate timestamp. The Task page
	// anchors the planning/summary streams here in the conversation timeline so
	// post-completion chat appends BELOW them, not above. Empty until confirmed.
	ConfirmedAt string `json:"confirmed_at,omitempty"`
	// ProjectID is the dependency project (role-sync source + PMO planning
	// context). Empty when the task is not bound to a project.
	ProjectID string `json:"project_id,omitempty"`
	// Coordinator (总控 / squad leader) attribution — who runs the planning +
	// summary streams shown in the main session, so the UI can label the
	// coordinator with its agent / runtime / model. Resolved from the squad's
	// leader agent.
	CoordinatorName            string `json:"coordinator_name,omitempty"`
	CoordinatorRuntimeName     string `json:"coordinator_runtime_name,omitempty"`
	CoordinatorRuntimeProvider string `json:"coordinator_runtime_provider,omitempty"`
	CoordinatorModel           string `json:"coordinator_model,omitempty"`
	CreatedAt                  string `json:"created_at"`
	UpdatedAt                  string `json:"updated_at"`
}

type GoalSubtaskResponse struct {
	ID              string   `json:"id"`
	GoalRunID       string   `json:"goal_run_id"`
	Seq             int32    `json:"seq"`
	Title           string   `json:"title"`
	Spec            string   `json:"spec"`
	AssigneeAgentID string   `json:"assignee_agent_id,omitempty"`
	DependsOn       []string `json:"depends_on"`
	Status          string   `json:"status"`
	Kind            string   `json:"kind"`
	Verdict         string   `json:"verdict,omitempty"`
	Attempt         int32    `json:"attempt"`
	MaxAttempts     int32    `json:"max_attempts"`
	FailureReason   string   `json:"failure_reason,omitempty"`
	// Result is the agent's final structured output (JSON). Surfaced so the ④
	// column can show what the subtask produced.
	Result json.RawMessage `json:"result,omitempty"`
	// Runtime handoff material injected into the daemon prompt for downstream
	// execute nodes with dependencies. Empty for root nodes and older tasks.
	UpstreamOutput string `json:"upstream_output,omitempty"`
	HandoffBrief   string `json:"handoff_brief,omitempty"`
	// TaskID is the execution task whose task_messages are this subtask's live
	// stream. Empty until the subtask is dispatched.
	TaskID string `json:"task_id,omitempty"`
	// Attribution — who/what ran this subtask, so the UI can show "which agent /
	// runtime / model responded" without extra round-trips. AgentName resolves
	// AssigneeAgentID; RuntimeName + RuntimeProvider come from that agent's
	// runtime; Model is the agent's configured model while running, upgraded to
	// the actually-used model (from task_usage) once the task reports usage.
	AgentName       string `json:"agent_name,omitempty"`
	RuntimeName     string `json:"runtime_name,omitempty"`
	RuntimeProvider string `json:"runtime_provider,omitempty"`
	Model           string `json:"model,omitempty"`
}

func (h *Handler) CreateGoal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req CreateGoalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.SquadID) == "" {
		writeError(w, http.StatusBadRequest, "squad_id is required")
		return
	}
	squadID, ok := parseUUIDOrBadRequest(w, req.SquadID, "squad_id")
	if !ok {
		return
	}

	var chatSessionID pgtype.UUID
	if req.ChatSessionID != nil && strings.TrimSpace(*req.ChatSessionID) != "" {
		parsed, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(*req.ChatSessionID), "chat_session_id")
		if !ok {
			return
		}
		chatSessionID = parsed
	}

	// Auto-decompose path: dispatch a planning task to the squad leader (PMO).
	// The leader decomposes via LLM and writes the plan back through SubmitPlan.
	if req.AutoDecompose {
		if len(req.Subtasks) > 0 {
			writeError(w, http.StatusBadRequest, "auto_decompose and subtasks are mutually exclusive")
			return
		}
		run, err := h.GoalService.StartPlanning(
			r.Context(),
			workspaceUUID, squadID, parseUUID(userID),
			chatSessionID,
			req.Title, req.Goal,
		)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Planning has no subtasks yet; the tree fills in once the plan lands.
		writeJSON(w, http.StatusCreated, goalRunToResponse(run, nil))
		return
	}

	subtasks := make([]service.SubtaskSpec, 0, len(req.Subtasks))
	for _, st := range req.Subtasks {
		var assignee pgtype.UUID
		if st.AssigneeAgentID != nil && strings.TrimSpace(*st.AssigneeAgentID) != "" {
			parsed, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(*st.AssigneeAgentID), "assignee_agent_id")
			if !ok {
				return
			}
			assignee = parsed
		}
		subtasks = append(subtasks, service.SubtaskSpec{
			Seq:             st.Seq,
			Title:           st.Title,
			Spec:            st.Spec,
			AssigneeAgentID: assignee,
			DependsOn:       st.DependsOn,
			Kind:            st.Kind,
		})
	}

	run, created, err := h.GoalService.CreateGoal(
		r.Context(),
		workspaceUUID, squadID, parseUUID(userID),
		chatSessionID,
		req.Title, req.Goal,
		subtasks, req.Confirmed,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, goalRunToResponse(run, created))
}

func (h *Handler) GetGoal(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	goalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}

	run, err := h.Queries.GetGoalRunInWorkspace(r.Context(), db.GetGoalRunInWorkspaceParams{
		ID:          goalID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "goal not found")
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

func (h *Handler) ConfirmGoal(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	goalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}

	// Ownership gate: the goal must live in this workspace.
	if _, err := h.Queries.GetGoalRunInWorkspace(r.Context(), db.GetGoalRunInWorkspaceParams{
		ID:          goalID,
		WorkspaceID: workspaceUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "goal not found")
		return
	}

	run, err := h.GoalService.ConfirmGoal(r.Context(), goalID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	subtasks, err := h.Queries.ListGoalSubtasks(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load subtasks")
		return
	}
	writeJSON(w, http.StatusOK, goalRunToResponse(run, subtasks))
}

// SubmitPlanRequest is the leader-produced decomposition written back via the
// CLI (`multica goal plan`). Same subtask shape as CreateGoalRequest.
type SubmitPlanRequest struct {
	Subtasks []CreateSubtaskInput `json:"subtasks"`
}

// SubmitPlan is the write-back target for the planning agent. It accepts the
// decomposition, persists the subtask DAG, flips the goal to executing, and
// dispatches root subtasks. Called by the squad leader's CLI during a planning
// task — so the authoring agent is the goal's own PMO, gated by workspace.
func (h *Handler) SubmitPlan(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	goalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}

	// Ownership gate: the goal must live in this workspace.
	if _, err := h.Queries.GetGoalRunInWorkspace(r.Context(), db.GetGoalRunInWorkspaceParams{
		ID:          goalID,
		WorkspaceID: workspaceUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "goal not found")
		return
	}

	var req SubmitPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Subtasks) == 0 {
		writeError(w, http.StatusBadRequest, "plan must contain at least one subtask")
		return
	}

	subtasks := make([]service.SubtaskSpec, 0, len(req.Subtasks))
	for _, st := range req.Subtasks {
		var assignee pgtype.UUID
		if st.AssigneeAgentID != nil && strings.TrimSpace(*st.AssigneeAgentID) != "" {
			parsed, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(*st.AssigneeAgentID), "assignee_agent_id")
			if !ok {
				return
			}
			assignee = parsed
		}
		subtasks = append(subtasks, service.SubtaskSpec{
			Seq:             st.Seq,
			Title:           st.Title,
			Spec:            st.Spec,
			AssigneeAgentID: assignee,
			DependsOn:       st.DependsOn,
			Kind:            st.Kind,
		})
	}

	run, created, err := h.GoalService.SubmitPlan(r.Context(), goalID, subtasks)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, goalRunToResponse(run, created))
}

// SubmitVerdictRequest is reported by a verify node's agent via the CLI
// (`multica goal verdict <subtask-id> pass|reject`).
type SubmitVerdictRequest struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// SubmitVerdict records a verify node's pass/reject verdict. The subtask id is
// the verify node (not the goal). The completion hook reads the verdict to
// drive the workflow.
func (h *Handler) SubmitVerdict(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	subtaskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "subtaskId"), "subtask id")
	if !ok {
		return
	}

	var req SubmitVerdictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.GoalService.SubmitVerdict(
		r.Context(), workspaceUUID, subtaskID,
		strings.TrimSpace(req.Verdict), strings.TrimSpace(req.Reason),
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, goalSubtaskToResponse(updated))
}

// PersistGoal dispatches a one-click snapshot of the task content into its bound
// project repo (the 持久化到工程 entry). Returns 202 with the dispatched persist
// task id; the agent authors the harness files on the daemon machine. Fails with
// 400 when the task is not bound to a project with a local repo.
func (h *Handler) PersistGoal(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	goalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}

	task, err := h.GoalService.PersistGoal(r.Context(), workspaceUUID, goalID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"persist_task_id": util.UUIDToString(task.ID),
	})
}

// DecideSubtaskRequest is the coordinator's 下一步判断 verdict on a failed node,
// reported via the CLI (`multica goal decide <subtask> proceed|reshape|abort`).
// Spec is the replacement spec, used only with 'reshape'.
type DecideSubtaskRequest struct {
	Decision string `json:"decision"`
	Spec     string `json:"spec"`
}

// DecideSubtask enacts the coordinator's next-step judgment on a failed node.
// The subtask id is the failed node. Returns the full updated goal so the UI
// tree refreshes (proceed/reshape re-activate downstream; abort blocks it).
func (h *Handler) DecideSubtask(w http.ResponseWriter, r *http.Request) {
	wsUUID, subtaskID, ok := h.parseSubtaskScope(w, r)
	if !ok {
		return
	}
	var req DecideSubtaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, err := h.GoalService.DecideSubtask(
		r.Context(), wsUUID, subtaskID,
		strings.TrimSpace(req.Decision), req.Spec,
	); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.writeGoalForSubtask(w, r, subtaskID)
}

// ---------------------------------------------------------------------------
// Human intervention on a failed / blocked subtask (escalation buttons).
// All return the full updated goal (run + subtasks) so the UI tree refreshes
// in one round-trip.
// ---------------------------------------------------------------------------

type ReassignSubtaskRequest struct {
	AgentID string `json:"agent_id"`
}

type EditSpecRequest struct {
	Spec string `json:"spec"`
}

// interventionGoalID resolves the parent goal of a subtask, gated on workspace,
// so the handler can return the whole refreshed goal after an intervention.
func (h *Handler) writeGoalForSubtask(w http.ResponseWriter, r *http.Request, subtaskID pgtype.UUID) {
	st, err := h.Queries.GetGoalSubtask(r.Context(), subtaskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload subtask")
		return
	}
	run, err := h.Queries.GetGoalRun(r.Context(), st.GoalRunID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload goal")
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

func (h *Handler) RetrySubtask(w http.ResponseWriter, r *http.Request) {
	wsUUID, subtaskID, ok := h.parseSubtaskScope(w, r)
	if !ok {
		return
	}
	if _, err := h.GoalService.RetrySubtask(r.Context(), wsUUID, subtaskID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.writeGoalForSubtask(w, r, subtaskID)
}

func (h *Handler) ReassignSubtask(w http.ResponseWriter, r *http.Request) {
	wsUUID, subtaskID, ok := h.parseSubtaskScope(w, r)
	if !ok {
		return
	}
	var req ReassignSubtaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.AgentID), "agent_id")
	if !ok {
		return
	}
	if _, err := h.GoalService.ReassignSubtask(r.Context(), wsUUID, subtaskID, agentID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.writeGoalForSubtask(w, r, subtaskID)
}

func (h *Handler) EditSubtaskSpec(w http.ResponseWriter, r *http.Request) {
	wsUUID, subtaskID, ok := h.parseSubtaskScope(w, r)
	if !ok {
		return
	}
	var req EditSpecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Spec) == "" {
		writeError(w, http.StatusBadRequest, "spec is required")
		return
	}
	if _, err := h.GoalService.EditSubtaskSpec(r.Context(), wsUUID, subtaskID, req.Spec); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.writeGoalForSubtask(w, r, subtaskID)
}

func (h *Handler) SkipSubtask(w http.ResponseWriter, r *http.Request) {
	wsUUID, subtaskID, ok := h.parseSubtaskScope(w, r)
	if !ok {
		return
	}
	if _, err := h.GoalService.SkipSubtask(r.Context(), wsUUID, subtaskID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.writeGoalForSubtask(w, r, subtaskID)
}

// TakeoverSubtask opens a human-takeover chat for a failed/blocked subtask and
// returns the new chat session, so the frontend can switch the chat surface to
// it. Unlike the other interventions it returns a ChatSession (not the goal),
// because the next step is "go talk to the agent", not "the tree changed".
func (h *Handler) TakeoverSubtask(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, subtaskID, ok := h.parseSubtaskScope(w, r)
	if !ok {
		return
	}
	session, err := h.GoalService.StartTakeover(r.Context(), wsUUID, subtaskID, parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, chatSessionToResponse(session))
}

// parseSubtaskScope resolves the workspace UUID + subtask UUID common to every
// intervention handler.
func (h *Handler) parseSubtaskScope(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	subtaskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "subtaskId"), "subtask id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return wsUUID, subtaskID, true
}

func goalRunToResponse(run db.GoalRun, subtasks []db.GoalSubtask) GoalRunResponse {
	out := GoalRunResponse{
		ID:          util.UUIDToString(run.ID),
		WorkspaceID: util.UUIDToString(run.WorkspaceID),
		SquadID:     util.UUIDToString(run.SquadID),
		Title:       run.Title,
		Goal:        run.Goal,
		Status:      run.Status,
		Subtasks:    make([]GoalSubtaskResponse, 0, len(subtasks)),
		CreatedAt:   timestampToString(run.CreatedAt),
		UpdatedAt:   timestampToString(run.UpdatedAt),
	}
	if run.ChatSessionID.Valid {
		out.ChatSessionID = util.UUIDToString(run.ChatSessionID)
	}
	if run.ConfirmedAt.Valid {
		out.ConfirmedAt = timestampToString(run.ConfirmedAt)
	}
	if run.ProjectID.Valid {
		out.ProjectID = util.UUIDToString(run.ProjectID)
	}
	for _, st := range subtasks {
		out.Subtasks = append(out.Subtasks, goalSubtaskToResponse(st))
	}
	return out
}

func goalSubtaskToResponse(st db.GoalSubtask) GoalSubtaskResponse {
	deps := make([]string, 0, len(st.DependsOn))
	for _, d := range st.DependsOn {
		deps = append(deps, util.UUIDToString(d))
	}
	out := GoalSubtaskResponse{
		ID:          util.UUIDToString(st.ID),
		GoalRunID:   util.UUIDToString(st.GoalRunID),
		Seq:         st.Seq,
		Title:       st.Title,
		Spec:        st.Spec,
		DependsOn:   deps,
		Status:      st.Status,
		Kind:        st.Kind,
		Attempt:     st.Attempt,
		MaxAttempts: st.MaxAttempts,
	}
	if st.AssigneeAgentID.Valid {
		out.AssigneeAgentID = util.UUIDToString(st.AssigneeAgentID)
	}
	if st.Verdict.Valid {
		out.Verdict = st.Verdict.String
	}
	if st.FailureReason.Valid {
		out.FailureReason = st.FailureReason.String
	}
	if len(st.Result) > 0 {
		out.Result = json.RawMessage(st.Result)
	}
	return out
}

// enrichGoalResponse fills the task-id fields (subtask execution task +
// planning task) so the UI can fetch their task_messages streams. Best-effort:
// missing tasks leave the ids empty.
func (h *Handler) enrichGoalResponse(ctx context.Context, resp *GoalRunResponse, run db.GoalRun) {
	if planID, err := h.Queries.GetPlanningTaskForGoal(ctx, util.UUIDToString(run.ID)); err == nil {
		resp.PlanningTaskID = util.UUIDToString(planID)
	}
	if sumID, err := h.Queries.GetSummaryTaskForGoal(ctx, util.UUIDToString(run.ID)); err == nil {
		resp.SummaryTaskID = util.UUIDToString(sumID)
	}
	if persistID, err := h.Queries.GetPersistTaskForGoal(ctx, util.UUIDToString(run.ID)); err == nil {
		resp.PersistTaskID = util.UUIDToString(persistID)
	}
	// CanPersist gates the 持久化到工程 button: a bound project with a local repo.
	if run.ProjectID.Valid {
		if resources, err := h.Queries.ListProjectResources(ctx, run.ProjectID); err == nil {
			for _, res := range resources {
				if res.ResourceType == "local_directory" {
					resp.CanPersist = true
					break
				}
			}
		}
	}
	// Attribution resolver with per-response caches (one goal can have many
	// subtasks sharing a few agents/runtimes — don't re-query each time).
	agentCache := map[string]db.Agent{}
	runtimeCache := map[string]db.AgentRuntime{}
	resolveAgent := func(id pgtype.UUID) (db.Agent, bool) {
		if !id.Valid {
			return db.Agent{}, false
		}
		key := util.UUIDToString(id)
		if a, ok := agentCache[key]; ok {
			return a, true
		}
		a, err := h.Queries.GetAgent(ctx, id)
		if err != nil {
			return db.Agent{}, false
		}
		agentCache[key] = a
		return a, true
	}
	resolveRuntime := func(id pgtype.UUID) (db.AgentRuntime, bool) {
		if !id.Valid {
			return db.AgentRuntime{}, false
		}
		key := util.UUIDToString(id)
		if rt, ok := runtimeCache[key]; ok {
			return rt, true
		}
		rt, err := h.Queries.GetAgentRuntime(ctx, id)
		if err != nil {
			return db.AgentRuntime{}, false
		}
		runtimeCache[key] = rt
		return rt, true
	}
	// actualModelForTask returns the model actually used (from task_usage) once
	// the agent reported it; "" while the task is still running / pre-usage.
	actualModelForTask := func(taskID pgtype.UUID) string {
		if !taskID.Valid {
			return ""
		}
		rows, err := h.Queries.GetTaskUsage(ctx, taskID)
		if err != nil || len(rows) == 0 {
			return ""
		}
		return rows[0].Model // ordered by model; first reported is sufficient for the label
	}

	for i := range resp.Subtasks {
		stID, perr := util.ParseUUID(resp.Subtasks[i].ID)
		if perr != nil {
			continue
		}
		var taskUUID pgtype.UUID
		if taskID, err := h.Queries.GetLatestTaskForSubtask(ctx, stID); err == nil {
			resp.Subtasks[i].TaskID = util.UUIDToString(taskID)
			taskUUID = taskID
			if task, err := h.Queries.GetAgentTask(ctx, taskID); err == nil && task.Context != nil {
				var gc service.GoalSubtaskContext
				if json.Unmarshal(task.Context, &gc) == nil && gc.Type == service.GoalSubtaskContextType {
					resp.Subtasks[i].UpstreamOutput = gc.UpstreamOutput
					resp.Subtasks[i].HandoffBrief = gc.HandoffBrief
				}
			}
		}
		// Attribution: agent name + its runtime + model (config now, actual once
		// task_usage lands — method 3).
		if agentID, err := util.ParseUUID(resp.Subtasks[i].AssigneeAgentID); err == nil {
			if agent, ok := resolveAgent(agentID); ok {
				resp.Subtasks[i].AgentName = agent.Name
				if agent.Model.Valid {
					resp.Subtasks[i].Model = agent.Model.String
				}
				if rt, ok := resolveRuntime(agent.RuntimeID); ok {
					resp.Subtasks[i].RuntimeName = rt.Name
					resp.Subtasks[i].RuntimeProvider = rt.Provider
				}
			}
		}
		if m := actualModelForTask(taskUUID); m != "" {
			resp.Subtasks[i].Model = m
		}
	}

	// Coordinator (总控) attribution: the squad leader runs planning + summary.
	if squad, err := h.Queries.GetSquad(ctx, run.SquadID); err == nil {
		if leader, ok := resolveAgent(squad.LeaderID); ok {
			resp.CoordinatorName = leader.Name
			if leader.Model.Valid {
				resp.CoordinatorModel = leader.Model.String
			}
			if rt, ok := resolveRuntime(leader.RuntimeID); ok {
				resp.CoordinatorRuntimeName = rt.Name
				resp.CoordinatorRuntimeProvider = rt.Provider
			}
			// Upgrade to the actually-used model from the planning/summary task.
			for _, tid := range []string{resp.PlanningTaskID, resp.SummaryTaskID} {
				if taskUUID, perr := util.ParseUUID(tid); perr == nil {
					if m := actualModelForTask(taskUUID); m != "" {
						resp.CoordinatorModel = m
					}
				}
			}
		}
	}
}
