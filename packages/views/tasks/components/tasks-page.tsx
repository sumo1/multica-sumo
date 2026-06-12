"use client";

import { useCallback, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, ListTree, ChevronDown, Users, FolderGit2, RefreshCw, Check } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { goalRunOptions, taskListOptions, goalKeys } from "@multica/core/goals/queries";
import { useTaskActions, useGoalIntervention, useSyncProjectRoles, usePersistGoal } from "@multica/core/goals/mutations";
import {
  chatMessagesOptions,
  pendingChatTaskOptions,
  chatKeys,
} from "@multica/core/chat/queries";
import { api } from "@multica/core/api";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { ChatMessageList } from "../../chat/components/chat-message-list";
import { ChatInput } from "../../chat/components/chat-input";
import { GoalStatusTree } from "../../assistant/components/goal-status-tree";
import { Markdown } from "../../common/markdown";
import { TaskStream } from "./task-stream";
import { useT } from "../../i18n";
import { createLogger } from "@multica/core/logger";
import type { GoalRun, GoalSubtask } from "@multica/core/types";

const logger = createLogger("tasks.page");

/** Terminal-success subtask count for the list/decomposition progress badge. */
function progressOf(goal: Pick<GoalRun, "subtasks">): { done: number; total: number } {
  const total = goal.subtasks.length;
  const done = goal.subtasks.filter((s) => s.status === "completed").length;
  return { done, total };
}

/**
 * Task mode page (design-task-mode.md). Two columns:
 *  ② task list  │  conversational main window (discussion / main / sub output)
 *
 * The main window is one continuous surface: the content area on top (discussion
 * chat, the PMO main session stream, or a subtask's output) and a pinned
 * ChatInput at the bottom that always talks to the PMO discussion session.
 * The status tree (task decomposition) lives in a pinnable Popover at the
 * top-right — its collapsed trigger carries an N/N progress badge.
 *
 * Tasks are created conversationally: the + button opens an empty discussion
 * with the PMO; the goal is described in the chat, not a form. The execution
 * engine (goal DAG / verify / intervene / takeover) is reused unchanged.
 */
export function TasksPage() {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const { data: tasks = [] } = useQuery(taskListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { create, confirm } = useTaskActions();
  const intervention = useGoalIntervention();
  const persist = usePersistGoal();

  // Which task is open in the main window. null = empty state.
  const [activeTaskId, setActiveTaskId] = useState<string | null>(null);
  // The discussion chat session id for the active task.
  const [discussionChatId, setDiscussionChatId] = useState<string | null>(null);
  // The main window shows the PMO main session by default; a subtask id switches
  // it to that subtask's output. null = main session.
  const [activeSubtaskId, setActiveSubtaskId] = useState<string | null>(null);

  const { data: goal } = useQuery({
    ...goalRunOptions(wsId, activeTaskId ?? ""),
    enabled: !!wsId && !!activeTaskId,
    refetchInterval: (q) => {
      const s = q.state.data?.status;
      return s === "planning" || s === "executing" ? 3000 : false;
    },
  });

  const { data: discussionMessages } = useQuery({
    ...chatMessagesOptions(discussionChatId ?? ""),
    enabled: !!discussionChatId,
  });
  const { data: discussionPending } = useQuery({
    ...pendingChatTaskOptions(discussionChatId ?? ""),
    enabled: !!discussionChatId,
  });

  const resolveAgentName = useMemo(() => {
    const byId = new Map(agents.map((a) => [a.id, a.name]));
    return (id: string) => byId.get(id);
  }, [agents]);

  // Conversational create: open an empty discussion with the PMO. No form —
  // the goal is described in the chat. Backend accepts empty title/goal/members
  // (CreateTask falls back to "任务讨论"); the PMO picks members during planning.
  const handleCreate = useCallback(async () => {
    try {
      const res = await create.mutateAsync({});
      if (res.goal?.id) {
        setActiveTaskId(res.goal.id);
        setDiscussionChatId(res.discussion_chat_id || null);
        setActiveSubtaskId(null);
        qc.invalidateQueries({ queryKey: goalKeys.list(wsId) });
      }
    } catch (e) {
      logger.error("create task failed", e);
    }
  }, [create, qc, wsId]);

  const handleSelectTask = useCallback((taskId: string, chatId: string) => {
    setActiveTaskId(taskId);
    setDiscussionChatId(chatId || null);
    setActiveSubtaskId(null);
  }, []);

  const handleSendDiscussion = useCallback(
    async (content: string, attachmentIds?: string[]) => {
      if (!discussionChatId) return;
      try {
        await api.sendChatMessage(discussionChatId, content, attachmentIds);
        qc.invalidateQueries({ queryKey: chatKeys.messages(discussionChatId) });
      } catch (e) {
        logger.error("send discussion message failed", e);
      }
    },
    [discussionChatId, qc],
  );

  // Stop the running discussion task. Mirrors chat-window's handleStop: clear
  // the pending pill optimistically (input unlocks immediately), then fire the
  // cancel POST. The backend marks the task cancelled + broadcasts task:cancelled
  // and the daemon interrupts the in-flight agent. Without this the stop button
  // was a no-op (the bug).
  const handleStopDiscussion = useCallback(() => {
    const taskId = discussionPending?.task_id;
    if (!taskId || !discussionChatId) return;
    qc.setQueryData(chatKeys.pendingTask(discussionChatId), {});
    qc.invalidateQueries({ queryKey: chatKeys.messages(discussionChatId) });
    api.cancelTaskById(taskId).then(
      () => logger.info("stop discussion task: cancelled", { taskId }),
      (err) => logger.warn("stop discussion task: cancel failed", err),
    );
  }, [discussionPending?.task_id, discussionChatId, qc]);

  const interveneHandlers = useMemo(
    () => ({
      onRetry: (id: string) => intervention.retry.mutate(id),
      onEditSpec: (id: string, spec: string) => intervention.editSpec.mutate({ subtaskId: id, spec }),
      onSkip: (id: string) => intervention.skip.mutate(id),
      onTakeover: (id: string) =>
        intervention.takeover.mutate(id, {
          onSuccess: () => qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) }),
        }),
    }),
    [intervention, qc, wsId],
  );

  const activeSubtask: GoalSubtask | undefined = goal?.subtasks.find((s) => s.id === activeSubtaskId);
  const treeProgress = goal ? progressOf(goal) : { done: 0, total: 0 };
  const treeHasFailure =
    goal?.subtasks.some((s) => s.status === "failed" || s.status === "blocked") ?? false;
  const showInput = !!discussionChatId;

  return (
    // h-full (not h-screen): the page mounts below the app top bar / tab strip,
    // so 100vh would push the bottom — and each column's scroll-container end —
    // off-screen. Fill the bounded route container instead.
    <div className="flex h-full min-h-0">
      {/* ② Task list */}
      <div className="flex min-h-0 w-64 shrink-0 flex-col border-r bg-muted/20">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h2 className="text-sm font-semibold">{t(($) => $.task_page.title)}</h2>
          <Button
            variant="ghost"
            size="icon-sm"
            className="rounded-full"
            aria-label={t(($) => $.task_page.new_task)}
            title={t(($) => $.task_page.new_task)}
            disabled={create.isPending}
            onClick={handleCreate}
          >
            <Plus className="size-4" />
          </Button>
        </div>
        <div className="flex-1 overflow-y-auto p-2">
          {tasks.length === 0 ? (
            <p className="px-2 py-6 text-center text-xs text-muted-foreground">
              {t(($) => $.task_page.empty_list)}
            </p>
          ) : (
            tasks.map((tk) => (
              <TaskListItem
                key={tk.id}
                task={tk}
                t={t}
                active={activeTaskId === tk.id}
                onSelect={() => handleSelectTask(tk.id, tk.chat_session_id)}
              />
            ))
          )}
        </div>
      </div>

      {/* Conversational main window (merged ③ + ④) */}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {activeTaskId && goal?.id ? (
          <>
            {/* Header: breadcrumb + confirm gate + pinnable decomposition tree */}
            <div className="flex items-center gap-2 border-b px-3 py-2">
              <div className="flex min-w-0 flex-1 items-center gap-1.5 text-xs font-medium text-muted-foreground">
                {activeSubtask ? (
                  <>
                    <button
                      type="button"
                      onClick={() => setActiveSubtaskId(null)}
                      className="text-primary hover:underline"
                    >
                      {t(($) => $.task_page.back_to_main)}
                    </button>
                    <span className="text-muted-foreground/60">/</span>
                    <span className="truncate">{activeSubtask.title}</span>
                  </>
                ) : (
                  <span className="truncate">{goal.title || t(($) => $.task_page.main_session)}</span>
                )}
              </div>

              {/* Members / roles entry — manage the task's dynamic squad:
                  sync roles from a dependency project, then add them as members
                  (the squad roster is what the PMO plans against). */}
              <MembersPopover goal={goal} t={t} resolveAgentName={resolveAgentName} />

              {goal.status === "discussion" && (
                <Button
                  size="sm"
                  className="h-7 shrink-0 text-xs"
                  disabled={confirm.isPending}
                  onClick={() => confirm.mutate(goal.id)}
                >
                  {t(($) => $.task_page.confirm_execute)}
                </Button>
              )}

              {/* 持久化到工程: one-click snapshot of the task content into the
                  bound project repo (harness structure). Always present; enabled
                  only when a local repo is bound (can_persist). The platform DB
                  stays the main truth — this is an on-demand, repeatable export. */}
              <Button
                variant="outline"
                size="sm"
                className="h-7 shrink-0 gap-1.5 text-xs"
                disabled={!goal.can_persist || persist.isPending}
                title={
                  goal.can_persist
                    ? t(($) => $.task_page.persist_hint)
                    : t(($) => $.task_page.persist_disabled_hint)
                }
                onClick={() => persist.mutate(goal.id)}
              >
                <FolderGit2 className="h-3.5 w-3.5" />
                {persist.isPending
                  ? t(($) => $.task_page.persisting)
                  : goal.persist_task_id
                    ? t(($) => $.task_page.persisted)
                    : t(($) => $.task_page.persist)}
              </Button>

              {/* Pinnable decomposition tree (top-right). Collapsed trigger shows
                  the N/N progress badge so status is scannable without opening. */}
              <Popover>
                <PopoverTrigger
                  render={
                    <Button variant="outline" size="sm" className="h-7 shrink-0 gap-1.5 text-xs" />
                  }
                >
                  <ListTree className="h-3.5 w-3.5" />
                  {t(($) => $.task_page.status_tree)}
                  {treeProgress.total > 0 && (
                    <span
                      className={cn(
                        "rounded px-1 text-[10px] font-semibold tabular-nums",
                        treeHasFailure
                          ? "bg-destructive/15 text-destructive"
                          : treeProgress.done === treeProgress.total
                            ? "bg-success/15 text-success"
                            : "bg-primary/15 text-primary",
                      )}
                    >
                      {treeProgress.done}/{treeProgress.total}
                    </span>
                  )}
                  <ChevronDown className="h-3 w-3 opacity-60" />
                </PopoverTrigger>
                <PopoverContent
                  align="end"
                  className="max-h-[70vh] w-[360px] overflow-y-auto p-0"
                >
                  <GoalStatusTree
                    goal={goal}
                    resolveAgentName={resolveAgentName}
                    selectedSubtaskId={activeSubtaskId}
                    onSelectMain={() => setActiveSubtaskId(null)}
                    onSelectSubtask={(id) => setActiveSubtaskId(id)}
                    intervene={interveneHandlers}
                  />
                </PopoverContent>
              </Popover>
            </div>

            {/* Content area. A subtask shows its own read-only output; the main
                view is ALWAYS the PMO discussion thread. Planning + the final
                deliverable are interleaved into the conversation at the confirm
                gate (timelineInsert anchored on confirmed_at), NOT pinned to the
                bottom — so chat sent after the task completes appends below the
                streams and the thread stays one continuous, time-ordered log. */}
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
              {activeSubtask ? (
                <div className="min-h-0 flex-1 overflow-y-auto">
                  <SubtaskOutput subtask={activeSubtask} t={t} />
                </div>
              ) : (
                <ChatMessageList
                  messages={discussionMessages ?? []}
                  pendingTask={discussionPending}
                  availability={undefined}
                  timelineInsert={
                    goal.planning_task_id
                      ? {
                          afterTs: goal.confirmed_at,
                          content: (
                            <div className="space-y-3 border-y py-3">
                              {goal.coordinator_name && (
                                <p className="truncate text-xs text-muted-foreground/70">
                                  {t(($) => $.goal.coordinator_label)}: {goal.coordinator_name}
                                  {[goal.coordinator_runtime_name, goal.coordinator_model]
                                    .filter(Boolean)
                                    .map((s) => ` · ${s}`)
                                    .join("")}
                                </p>
                              )}
                              <TaskStream
                                taskId={goal.planning_task_id}
                                running={goal.status === "planning"}
                                emptyHint={t(($) => $.task_page.planning_hint)}
                              />
                              {goal.summary_task_id && (
                                <SummarySection
                                  taskId={goal.summary_task_id}
                                  running={goal.status === "executing"}
                                  title={t(($) => $.task_page.final_summary)}
                                  emptyHint={t(($) => $.task_page.summarizing)}
                                />
                              )}
                            </div>
                          ),
                        }
                      : undefined
                  }
                />
              )}
            </div>

            {/* Pinned input — always talks to the PMO discussion session, so the
                user can keep the conversation going even after the task is done
                or while viewing a subtask's read-only output. */}
            {showInput && (
              <ChatInput
                onSend={handleSendDiscussion}
                onUploadFile={async () => null}
                onStop={handleStopDiscussion}
                isRunning={!!discussionPending?.task_id}
                disabled={false}
                noAgent={false}
              />
            )}
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center p-6">
            <div className="max-w-xs text-center">
              <p className="text-sm text-muted-foreground">{t(($) => $.task_page.empty_detail)}</p>
              <Button className="mt-4" disabled={create.isPending} onClick={handleCreate}>
                <Plus className="size-4" />
                {t(($) => $.task_page.new_task)}
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

/** Members / roles popover: the task's member-configuration center.
 *  - Pick a dependency project and sync its repo roles into workspace Agents.
 *  - Add agents (incl. freshly synced ones) to the task's dynamic squad — the
 *    squad roster is exactly what the PMO plans against.
 *  This realizes "目标 → 工程 → 成员" without a separate menu: roles flow from
 *  the project into the task. */
function MembersPopover({
  goal,
  t,
  resolveAgentName,
}: {
  goal: GoalRun;
  t: ReturnType<typeof useT<"chat">>["t"];
  resolveAgentName: (id: string) => string | undefined;
}) {
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { addMember } = useTaskActions();
  const syncRoles = useSyncProjectRoles();

  // The project to sync roles from: the goal's bound project if any, else the
  // user's pick. Drives the sync button.
  const [pickedProjectId, setPickedProjectId] = useState<string>(goal.project_id || "");
  const activeProjectId = goal.project_id || pickedProjectId;
  const activeAgents = useMemo(() => agents.filter((a) => !a.archived_at), [agents]);
  // Agents added to this task's squad in-session (optimistic check marks).
  const [added, setAdded] = useState<Set<string>>(new Set());

  return (
    <Popover>
      <PopoverTrigger
        render={<Button variant="outline" size="sm" className="h-7 shrink-0 gap-1.5 text-xs" />}
      >
        <Users className="h-3.5 w-3.5" />
        {t(($) => $.task_page.members)}
        <ChevronDown className="h-3 w-3 opacity-60" />
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[340px] p-3">
        {/* Sync roles from a dependency project */}
        <div className="mb-3">
          <div className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
            <FolderGit2 className="h-3.5 w-3.5" />
            {t(($) => $.task_page.sync_from_project)}
          </div>
          <div className="flex items-center gap-1.5">
            <select
              value={activeProjectId}
              onChange={(e) => setPickedProjectId(e.target.value)}
              className="h-7 min-w-0 flex-1 rounded-md border bg-background px-2 text-xs"
            >
              <option value="">{t(($) => $.task_page.pick_project)}</option>
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.title}
                </option>
              ))}
            </select>
            <Button
              size="sm"
              variant="outline"
              className="h-7 shrink-0 gap-1 text-xs"
              disabled={!activeProjectId || syncRoles.isPending}
              onClick={() => activeProjectId && syncRoles.mutate(activeProjectId)}
            >
              <RefreshCw className={cn("h-3.5 w-3.5", syncRoles.isPending && "animate-spin")} />
              {t(($) => $.task_page.sync)}
            </Button>
          </div>
          {syncRoles.data && (
            <p className="mt-1 text-[11px] text-muted-foreground">
              {t(($) => $.task_page.sync_result, {
                created: syncRoles.data.created.length,
                updated: syncRoles.data.updated.length,
              })}
            </p>
          )}
        </div>

        {/* Add agents (roles) to the task's squad */}
        <div className="mb-1 text-xs font-medium text-muted-foreground">
          {t(($) => $.task_page.add_members)}
        </div>
        <div className="max-h-64 space-y-0.5 overflow-y-auto">
          {activeAgents.length === 0 ? (
            <p className="py-3 text-center text-xs text-muted-foreground">
              {t(($) => $.task_page.no_roles)}
            </p>
          ) : (
            activeAgents.map((a) => {
              const isAdded = added.has(a.id);
              return (
                <button
                  key={a.id}
                  type="button"
                  disabled={isAdded || addMember.isPending}
                  onClick={() =>
                    addMember.mutate(
                      { taskId: goal.id, agentId: a.id },
                      { onSuccess: () => setAdded((prev) => new Set(prev).add(a.id)) },
                    )
                  }
                  className={cn(
                    "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted/60 disabled:opacity-60",
                  )}
                >
                  <span className="min-w-0 flex-1 truncate">
                    {resolveAgentName(a.id) ?? a.name}
                  </span>
                  {isAdded ? (
                    <Check className="h-3.5 w-3.5 shrink-0 text-success" />
                  ) : (
                    <Plus className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  )}
                </button>
              );
            })
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

/** ② list item: title + status pill + N/N progress dot. Status text is
 *  localized (enum drift falls through to the raw string, never crashes). */
function TaskListItem({
  task,
  t,
  active,
  onSelect,
}: {
  task: GoalRun;
  t: ReturnType<typeof useT<"chat">>["t"];
  active: boolean;
  onSelect: () => void;
}) {
  const { done, total } = progressOf(task);
  const hasFailure = task.subtasks.some((s) => s.status === "failed" || s.status === "blocked");

  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "mb-1 flex w-full flex-col items-start gap-1 rounded-md px-2 py-2 text-left transition-colors hover:bg-muted/60",
        active && "bg-muted",
      )}
    >
      <span className="w-full truncate text-sm font-medium text-foreground">
        {task.title || t(($) => $.goal.untitled)}
      </span>
      <span className="flex w-full items-center gap-1.5 text-xs text-muted-foreground">
        <span
          className={cn(
            "size-1.5 shrink-0 rounded-full",
            hasFailure
              ? "bg-destructive"
              : task.status === "completed"
                ? "bg-success"
                : task.status === "executing" || task.status === "planning"
                  ? "bg-primary"
                  : "bg-muted-foreground/40",
          )}
        />
        <span className="truncate">{taskStatusLabel(t, task.status)}</span>
        {total > 0 && (
          <span className="ml-auto shrink-0 tabular-nums text-muted-foreground/70">
            {done}/{total}
          </span>
        )}
      </span>
    </button>
  );
}

/** Localized goal_run status; enum drift downgrades to the raw value. */
function taskStatusLabel(
  t: ReturnType<typeof useT<"chat">>["t"],
  status: GoalRun["status"],
): string {
  return t(($) => $.task_page.run_status[status as keyof typeof $.task_page.run_status]) || status;
}

/** The final-deliverable section appended to the main session. Carries strong
 *  visual weight (left accent + card) because the close-out is the global
 *  headline result, not just another stream. */
function SummarySection({
  taskId,
  running,
  title,
  emptyHint,
}: {
  taskId: string;
  running?: boolean;
  title: string;
  emptyHint: string;
}) {
  return (
    <div className="rounded-lg border border-primary/30 bg-primary/[0.03]">
      <div className="border-b border-primary/20 px-3 py-2 text-xs font-semibold uppercase tracking-wide text-primary">
        {title}
      </div>
      <TaskStream taskId={taskId} running={running} emptyHint={emptyHint} />
    </div>
  );
}

/** A subtask's view: spec header + live execution stream (task_messages) +
 *  final result/failure. */
function SubtaskOutput({
  subtask,
  t,
}: {
  subtask: GoalSubtask;
  t: ReturnType<typeof useT<"chat">>["t"];
}) {
  // Extract the agent's final output text from the structured result. Used
  // only as a fallback when the subtask has no execution stream (task_messages),
  // since the stream's <TimelineView> already renders the final answer.
  let resultText = "";
  if (subtask.result) {
    const r = subtask.result as Record<string, unknown> | string;
    if (typeof r === "string") resultText = r;
    else if (r && typeof r === "object" && typeof r.output === "string") resultText = r.output;
    else resultText = JSON.stringify(r, null, 2);
  }

  // Attribution: which agent / runtime / model ran this subtask.
  const attribution = [subtask.agent_name, subtask.runtime_name, subtask.model]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="flex flex-col gap-3 p-3 text-sm">
      <div className="border-b pb-2">
        <h4 className="mb-1 font-medium text-foreground">{subtask.title}</h4>
        {attribution && (
          <p className="mb-1 truncate text-xs text-muted-foreground/70">{attribution}</p>
        )}
        <p className="whitespace-pre-wrap text-xs text-muted-foreground">{subtask.spec}</p>
      </div>

      {subtask.failure_reason && (
        <div className="rounded border border-destructive/40 bg-destructive/5 p-2 text-xs text-destructive">
          {subtask.failure_reason}
        </div>
      )}

      {(subtask.handoff_brief || subtask.upstream_output) && (
        <div className="space-y-2 rounded border bg-muted/20 p-2">
          {subtask.handoff_brief && (
            <div>
              <div className="mb-1 text-xs font-medium text-foreground">
                {t(($) => $.task_page.handoff_brief)}
              </div>
              <p className="whitespace-pre-wrap break-words text-xs text-muted-foreground">
                {subtask.handoff_brief}
              </p>
            </div>
          )}
          {subtask.upstream_output && (
            <details open className="group">
              <summary className="flex cursor-pointer list-none items-center gap-1 text-xs font-medium text-foreground">
                <ChevronDown className="h-3 w-3 transition-transform group-open:rotate-180" />
                <span>{t(($) => $.task_page.upstream_input)}</span>
              </summary>
              <div className="mt-2 max-h-48 overflow-y-auto whitespace-pre-wrap break-words rounded bg-background/60 p-2 text-xs text-muted-foreground">
                {subtask.upstream_output}
              </div>
            </details>
          )}
        </div>
      )}

      {subtask.task_id ? (
        // Full transcript: thinking fold + markdown final answer.
        <TaskStream
          taskId={subtask.task_id}
          running={subtask.status === "running"}
          emptyHint={t(($) => $.task_page.sub_session)}
        />
      ) : (
        // Fallback for runs without a captured stream: render the stored
        // result as markdown rather than a raw blob.
        resultText && (
          <div className="prose prose-sm dark:prose-invert max-w-none">
            <Markdown>{resultText}</Markdown>
          </div>
        )
      )}
    </div>
  );
}
