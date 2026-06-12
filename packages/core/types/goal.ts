/**
 * Goal mode (PMO orchestration). A goal_run is one PMO-led unit of work on a
 * squad; it decomposes into a DAG of subtasks. Shapes mirror the server's
 * GoalRunResponse / GoalSubtaskResponse (server/internal/handler/goal.go).
 */

export type GoalRunStatus =
  | "discussion"
  | "confirmed"
  | "planning"
  | "executing"
  | "completed"
  | "partial"
  | "failed"
  | "cancelled";

export type GoalSubtaskStatus =
  | "pending"
  | "ready"
  | "running"
  | "completed"
  | "failed"
  | "blocked"
  | "skipped";

/** 'execute' nodes do the work; 'verify' nodes adversarially review upstream. */
export type GoalSubtaskKind = "execute" | "verify";

/** A verify node's outcome. Empty until the verifier reports. */
export type GoalVerdict = "" | "pass" | "reject";

export interface GoalSubtask {
  id: string;
  goal_run_id: string;
  seq: number;
  title: string;
  spec: string;
  /** Executing role agent. Empty string when not yet assigned. */
  assignee_agent_id: string;
  /** Ids of sibling subtasks that must complete before this one runs. */
  depends_on: string[];
  status: GoalSubtaskStatus;
  /** Node type. Verify nodes review the output of the nodes they depend on. */
  kind: GoalSubtaskKind;
  /** Verify nodes only: pass / reject / "" (not yet judged). */
  verdict: GoalVerdict;
  attempt: number;
  max_attempts: number;
  /** Non-empty only when status is failed. */
  failure_reason: string;
  /** Agent's final structured output (JSON object), if produced. */
  result?: unknown;
  /** Direct runtime handoff from upstream subtasks, injected into this prompt. */
  upstream_output: string;
  /** Short frame telling the agent how to use upstream_output. */
  handoff_brief: string;
  /** Execution task id — fetch its task_messages for the live output stream. */
  task_id: string;
  /** Attribution: which agent / runtime / model ran this subtask. agent_name
   * resolves assignee_agent_id; runtime_name + runtime_provider come from that
   * agent's runtime; model is the configured model while running, upgraded to
   * the actually-used model once the task reports usage. Empty when unresolved
   * (older server, or not yet assigned). */
  agent_name: string;
  runtime_name: string;
  runtime_provider: string;
  model: string;
}

export interface GoalRun {
  id: string;
  workspace_id: string;
  squad_id: string;
  /** Conversation this goal is driven from. Empty when standalone. */
  chat_session_id: string;
  title: string;
  goal: string;
  status: GoalRunStatus;
  subtasks: GoalSubtask[];
  /** PMO planning task id — its task_messages are the main session stream. */
  planning_task_id: string;
  /** PMO summary task id — the final deliverable, shown as the tail of the
   * main session. Empty until all subtasks finish and the summary runs. */
  summary_task_id: string;
  /** Discussion → execution gate timestamp. The Task page anchors the
   * planning/summary streams at this point in the conversation timeline, so
   * chat sent after completion appends below them. Empty until confirmed. */
  confirmed_at: string;
  /** Dependency project (role-sync source + PMO planning context). Empty when
   * the task is not bound to a project. */
  project_id: string;
  /** Most recent repo-persist (one-click snapshot) task id. Empty until the
   * user clicks 持久化到工程 at least once. */
  persist_task_id: string;
  /** True when the task is bound to a project carrying a local repo
   * (local_directory) — the precondition for the 持久化到工程 button. */
  can_persist: boolean;
  /** Coordinator (总控 / squad leader) attribution — who runs the planning +
   * summary streams in the main session. Empty when unresolved. */
  coordinator_name: string;
  coordinator_runtime_name: string;
  coordinator_runtime_provider: string;
  coordinator_model: string;
  created_at: string;
  updated_at: string;
}

/** Task-mode create response: the goal + its discussion chat to open. */
export interface TaskCreateResult {
  goal: GoalRun;
  discussion_chat_id: string;
}

/** Result of syncing role definitions from a project's repo into Agents. */
export interface RoleSyncResult {
  created: string[];
  updated: string[];
  skipped: string[];
}
