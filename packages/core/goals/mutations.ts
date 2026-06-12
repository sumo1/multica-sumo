import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { createLogger } from "../logger";
import { goalKeys } from "./queries";
import { chatKeys } from "../chat/queries";
import { workspaceKeys } from "../workspace/queries";
import type { GoalRun } from "../types";

const logger = createLogger("goal.mut");

export interface CreateGoalInput {
  squad_id: string;
  title?: string;
  goal?: string;
  chat_session_id?: string;
  confirmed?: boolean;
  /** Dispatch a planning task to the PMO to decompose the goal via LLM. */
  auto_decompose?: boolean;
  subtasks?: Array<{
    seq: number;
    title: string;
    spec: string;
    assignee_agent_id?: string;
    depends_on?: number[];
  }>;
}

export function useCreateGoal() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (data: CreateGoalInput) => {
      logger.info("createGoal.start", {
        squad_id: data.squad_id,
        subtasks: data.subtasks?.length ?? 0,
        confirmed: !!data.confirmed,
      });
      return api.createGoal(data);
    },
    onSuccess: (run: GoalRun) => {
      logger.info("createGoal.success", { goalId: run.id, status: run.status });
      // Seed the run cache so the status tree renders immediately.
      qc.setQueryData(goalKeys.run(wsId, run.id), run);
    },
    onError: (err) => {
      logger.error("createGoal.error", err);
    },
  });
}

export interface CreateTaskInput {
  title?: string;
  goal?: string;
  members?: string[];
  /** Optional dependency project — its repo is the role-sync source and the
   *  PMO's planning context. */
  project_id?: string;
}

/** Task mode: create (discussion phase), add member, confirm (→ planning). */
export function useTaskActions() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  const create = useMutation({
    mutationFn: (data: CreateTaskInput) => {
      logger.info("createTask.start", { members: data.members?.length ?? 0 });
      return api.createTask(data);
    },
    onSuccess: (res) => {
      if (res.goal?.id) qc.setQueryData(goalKeys.run(wsId, res.goal.id), res.goal);
    },
    onError: (err) => logger.error("createTask.error", err),
  });

  const addMember = useMutation({
    mutationFn: (vars: { taskId: string; agentId: string }) =>
      api.addTaskMember(vars.taskId, vars.agentId),
    onSuccess: (run) => {
      if (run?.id) qc.setQueryData(goalKeys.run(wsId, run.id), run);
    },
    onError: (err) => logger.error("addTaskMember.error", err),
  });

  const confirm = useMutation({
    mutationFn: (taskId: string) => api.confirmTask(taskId),
    onSuccess: (run) => {
      if (run?.id) qc.setQueryData(goalKeys.run(wsId, run.id), run);
    },
    onError: (err) => logger.error("confirmTask.error", err),
  });

  return { create, addMember, confirm };
}

/** Sync a project's repo role definitions into workspace Agents. On success,
 *  invalidates the agent list so the new roles appear in the member pool. */
export function useSyncProjectRoles() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (projectId: string) => api.syncProjectRoles(projectId),
    onSuccess: (res) => {
      logger.info("syncProjectRoles.success", {
        created: res.created.length,
        updated: res.updated.length,
        skipped: res.skipped.length,
      });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
    onError: (err) => logger.error("syncProjectRoles.error", err),
  });
}

/**
 * One-click persist (snapshot) of the task content into its bound project repo
 * (the 持久化到工程 entry). Dispatches a persist task to the coordinator; on
 * success we invalidate the run so persist_task_id surfaces. The platform DB
 * stays the main truth — this is an on-demand, repeatable export, not a sync.
 */
export function usePersistGoal() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (goalId: string) => {
      logger.info("persistGoal.start", { goalId });
      return api.persistGoal(goalId);
    },
    onSuccess: (res, goalId) => {
      logger.info("persistGoal.success", { goalId, taskId: res.persist_task_id });
      qc.invalidateQueries({ queryKey: goalKeys.run(wsId, goalId) });
    },
    onError: (err) => {
      logger.error("persistGoal.error", err);
    },
  });
}

export function useConfirmGoal() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (goalId: string) => {
      logger.info("confirmGoal.start", { goalId });
      return api.confirmGoal(goalId);
    },
    onSuccess: (run: GoalRun) => {
      logger.info("confirmGoal.success", { goalId: run.id, status: run.status });
      qc.setQueryData(goalKeys.run(wsId, run.id), run);
    },
    onError: (err) => {
      logger.error("confirmGoal.error", err);
    },
  });
}

/**
 * Human interventions on a failed / blocked subtask. Each returns the full
 * updated goal, which we seed back into the run cache so the status tree
 * reflects the change immediately (WS goal:* events also invalidate as backup).
 */
export function useGoalIntervention() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  const seed = (run: GoalRun) => {
    if (run.id) qc.setQueryData(goalKeys.run(wsId, run.id), run);
  };

  const retry = useMutation({
    mutationFn: (subtaskId: string) => api.retryGoalSubtask(subtaskId),
    onSuccess: seed,
    onError: (err) => logger.error("retryGoalSubtask.error", err),
  });

  const reassign = useMutation({
    mutationFn: (vars: { subtaskId: string; agentId: string }) =>
      api.reassignGoalSubtask(vars.subtaskId, vars.agentId),
    onSuccess: seed,
    onError: (err) => logger.error("reassignGoalSubtask.error", err),
  });

  const editSpec = useMutation({
    mutationFn: (vars: { subtaskId: string; spec: string }) =>
      api.editGoalSubtaskSpec(vars.subtaskId, vars.spec),
    onSuccess: seed,
    onError: (err) => logger.error("editGoalSubtaskSpec.error", err),
  });

  const skip = useMutation({
    mutationFn: (subtaskId: string) => api.skipGoalSubtask(subtaskId),
    onSuccess: seed,
    onError: (err) => logger.error("skipGoalSubtask.error", err),
  });

  // Takeover returns a chat session, not a goal — invalidate the session list
  // so it shows up; the caller activates it to switch the chat surface.
  const takeover = useMutation({
    mutationFn: (subtaskId: string) => api.takeoverGoalSubtask(subtaskId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
    onError: (err) => logger.error("takeoverGoalSubtask.error", err),
  });

  return { retry, reassign, editSpec, skip, takeover };
}
