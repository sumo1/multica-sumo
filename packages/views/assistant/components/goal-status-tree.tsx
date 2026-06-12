"use client";

import * as React from "react";
import { useState } from "react";
import {
  CheckCircle2,
  Circle,
  Loader2,
  XCircle,
  Ban,
  CircleDot,
  Target,
  ShieldCheck,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { GoalRun, GoalSubtask, GoalSubtaskStatus } from "@multica/core/types";
import { useT } from "../../i18n";

type GoalT = ReturnType<typeof useT<"chat">>["t"];

// ---------------------------------------------------------------------------
// Goal status tree (column ④). Renders the Codex-style overall progress bar
// plus the PMO role tree: Goal → subtasks → role status. Status icons mirror
// the Claude Code agent-view vocabulary (✓ done / ◌ running / ○ pending /
// ✗ failed / ⊘ blocked). Purely presentational — the parent passes a GoalRun.
// ---------------------------------------------------------------------------

interface SubtaskIconProps {
  status: GoalSubtaskStatus;
}

function SubtaskIcon({ status }: SubtaskIconProps) {
  switch (status) {
    case "completed":
      return <CheckCircle2 className="h-4 w-4 text-success" aria-label="completed" />;
    case "running":
      return <Loader2 className="h-4 w-4 animate-spin text-primary" aria-label="running" />;
    case "ready":
      return <CircleDot className="h-4 w-4 text-primary" aria-label="ready" />;
    case "failed":
      return <XCircle className="h-4 w-4 text-destructive" aria-label="failed" />;
    case "blocked":
      return <Ban className="h-4 w-4 text-muted-foreground" aria-label="blocked" />;
    case "skipped":
      return <Ban className="h-4 w-4 text-muted-foreground" aria-label="skipped" />;
    case "pending":
    default:
      // Enum drift downgrades here too: an unknown status renders the neutral
      // pending circle instead of breaking the tree.
      return <Circle className="h-4 w-4 text-muted-foreground" aria-label="pending" />;
  }
}

/** Count terminal-success subtasks for the progress headline. */
function countProgress(subtasks: GoalSubtask[]): { done: number; total: number } {
  const total = subtasks.length;
  const done = subtasks.filter((s) => s.status === "completed").length;
  return { done, total };
}

const STATUS_KEY: Record<GoalSubtaskStatus, string> = {
  pending: "pending",
  ready: "ready",
  running: "running",
  completed: "completed",
  failed: "failed",
  blocked: "blocked",
  skipped: "skipped",
};

function statusLabel(t: GoalT, status: GoalSubtaskStatus): string {
  // Enum drift: an unknown status falls through to the raw string instead of
  // throwing. The icon already downgrades to the pending circle.
  const key = STATUS_KEY[status];
  if (!key) return status;
  return t(($) => $.goal.status[key as keyof typeof $.goal.status]);
}

/** Optional intervention callbacks; when provided, failed/blocked nodes show
 * the escalation buttons. busy disables them during an in-flight op. */
export interface GoalInterventionHandlers {
  onRetry?: (subtaskId: string) => void;
  onReassign?: (subtaskId: string) => void;
  onEditSpec?: (subtaskId: string, spec: string) => void;
  onSkip?: (subtaskId: string) => void;
  /** Open a hands-on chat with the failed node's agent. */
  onTakeover?: (subtaskId: string) => void;
  busySubtaskId?: string | null;
}

interface GoalSubtaskRowProps {
  subtask: GoalSubtask;
  t: GoalT;
  /** Resolve an agent id to a display name; falls back to a short id. */
  resolveAgentName?: (agentId: string) => string | undefined;
  selected?: boolean;
  onSelect?: (subtaskId: string) => void;
  intervene?: GoalInterventionHandlers;
}

function GoalSubtaskRow({ subtask, t, resolveAgentName, selected, onSelect, intervene }: GoalSubtaskRowProps) {
  const [editing, setEditing] = useState(false);
  const [draftSpec, setDraftSpec] = useState(subtask.spec);

  // Prefer the API-resolved agent name (it also carries runtime/model); fall
  // back to the prop resolver, then a truncated id.
  const agentName = subtask.agent_name
    || (subtask.assignee_agent_id
      ? resolveAgentName?.(subtask.assignee_agent_id) ?? `${subtask.assignee_agent_id.slice(0, 8)}…`
      : t(($) => $.goal.unassigned));

  // Attribution suffix: "· <runtime> · <model>" — which runtime/model ran this
  // node. Only the parts the server resolved are shown.
  const attribution = [subtask.runtime_name, subtask.model].filter(Boolean).join(" · ");

  const blocked = subtask.status === "blocked";
  const failed = subtask.status === "failed";
  const isVerify = subtask.kind === "verify";

  // Interventions are offered on terminal-but-recoverable nodes.
  const canIntervene = !!intervene && (failed || blocked);
  const busy = intervene?.busySubtaskId === subtask.id;

  return (
    <div
      className={cn(
        "rounded-md transition-colors",
        selected && "bg-muted",
        isVerify && "border-l-2 border-warning/60",
      )}
    >
      <button
        type="button"
        onClick={() => onSelect?.(subtask.id)}
        className={cn(
          "flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted/60",
          isVerify && "pl-1.5",
        )}
      >
        <span className="mt-0.5 shrink-0">
          <SubtaskIcon status={subtask.status} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1.5">
            {isVerify && (
              <ShieldCheck
                className="h-3.5 w-3.5 shrink-0 text-warning"
                aria-label={t(($) => $.goal.verify_node)}
              />
            )}
            <span className="truncate font-medium text-foreground">
              {subtask.title || t(($) => $.goal.untitled_subtask)}
            </span>
            {isVerify && subtask.verdict && (
              <span
                className={cn(
                  "shrink-0 rounded px-1 text-[10px] font-semibold uppercase",
                  subtask.verdict === "pass"
                    ? "bg-success/15 text-success"
                    : "bg-destructive/15 text-destructive",
                )}
              >
                {subtask.verdict === "pass"
                  ? t(($) => $.goal.verdict_pass)
                  : t(($) => $.goal.verdict_reject)}
              </span>
            )}
            <span
              className={cn(
                "ml-auto shrink-0 text-xs",
                failed ? "text-destructive" : "text-muted-foreground",
              )}
            >
              {statusLabel(t, subtask.status)}
            </span>
          </span>
          <span className="block truncate text-xs text-muted-foreground">
            {agentName}
            {attribution && (
              <span className="text-muted-foreground/70"> · {attribution}</span>
            )}
            {blocked && subtask.depends_on.length > 0 && (
              <span className="ml-1">{t(($) => $.goal.depends_unmet)}</span>
            )}
          </span>
          {failed && subtask.failure_reason && (
            <span className="mt-0.5 block truncate text-xs text-destructive">
              {subtask.failure_reason}
              {subtask.attempt > 0 &&
                ` ${t(($) => $.goal.retried, { attempt: subtask.attempt, max: subtask.max_attempts })}`}
            </span>
          )}
        </span>
      </button>

      {/* Intervention footer — only on failed/blocked nodes when handlers given */}
      {canIntervene && (
        <div className="px-2 pb-1.5 pl-8">
          {editing ? (
            <div className="flex flex-col gap-1">
              <textarea
                value={draftSpec}
                onChange={(e) => setDraftSpec(e.target.value)}
                placeholder={t(($) => $.goal.intervene.edit_spec_placeholder)}
                className="min-h-[60px] w-full resize-none rounded border bg-background p-1.5 text-xs"
              />
              <div className="flex gap-1">
                <button
                  type="button"
                  disabled={busy || !draftSpec.trim()}
                  onClick={() => {
                    intervene?.onEditSpec?.(subtask.id, draftSpec.trim());
                    setEditing(false);
                  }}
                  className="rounded bg-primary px-2 py-0.5 text-xs text-primary-foreground disabled:opacity-50"
                >
                  {t(($) => $.goal.intervene.edit_spec_save)}
                </button>
                <button
                  type="button"
                  onClick={() => { setEditing(false); setDraftSpec(subtask.spec); }}
                  className="rounded px-2 py-0.5 text-xs text-muted-foreground hover:bg-muted"
                >
                  {t(($) => $.goal.intervene.edit_spec_cancel)}
                </button>
              </div>
            </div>
          ) : (
            <div className="flex flex-wrap gap-1">
              {intervene?.onRetry && (
                <InterventionButton t={t} disabled={busy} onClick={() => intervene.onRetry?.(subtask.id)}>
                  {t(($) => $.goal.intervene.retry)}
                </InterventionButton>
              )}
              {intervene?.onReassign && (
                <InterventionButton t={t} disabled={busy} onClick={() => intervene.onReassign?.(subtask.id)}>
                  {t(($) => $.goal.intervene.reassign)}
                </InterventionButton>
              )}
              {intervene?.onEditSpec && (
                <InterventionButton t={t} disabled={busy} onClick={() => { setDraftSpec(subtask.spec); setEditing(true); }}>
                  {t(($) => $.goal.intervene.edit_spec)}
                </InterventionButton>
              )}
              {intervene?.onSkip && (
                <InterventionButton t={t} disabled={busy} onClick={() => intervene.onSkip?.(subtask.id)}>
                  {t(($) => $.goal.intervene.skip)}
                </InterventionButton>
              )}
              {intervene?.onTakeover && (
                <InterventionButton t={t} disabled={busy} onClick={() => intervene.onTakeover?.(subtask.id)}>
                  {t(($) => $.goal.intervene.takeover)}
                </InterventionButton>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function InterventionButton({
  children,
  disabled,
  onClick,
  t,
}: {
  children: React.ReactNode;
  disabled?: boolean;
  onClick: () => void;
  t: GoalT;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className="rounded border px-2 py-0.5 text-xs text-foreground transition-colors hover:bg-muted disabled:opacity-50"
    >
      {disabled ? t(($) => $.goal.intervene.working) : children}
    </button>
  );
}

export interface GoalStatusTreeProps {
  goal: GoalRun;
  resolveAgentName?: (agentId: string) => string | undefined;
  selectedSubtaskId?: string | null;
  onSelectSubtask?: (subtaskId: string) => void;
  /** Activate the main (PMO) session. The overall-progress header doubles as
   * the "main task" entry — clicking it returns the ④ view to the PMO stream,
   * replacing the old breadcrumb. Highlighted when no subtask is selected. */
  onSelectMain?: () => void;
  /** When provided, failed/blocked nodes show retry/reassign/edit-spec/skip. */
  intervene?: GoalInterventionHandlers;
  className?: string;
}

export function GoalStatusTree({
  goal,
  resolveAgentName,
  selectedSubtaskId,
  onSelectSubtask,
  onSelectMain,
  intervene,
  className,
}: GoalStatusTreeProps) {
  const { t } = useT("chat");
  const { done, total } = countProgress(goal.subtasks);
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  const hasFailure = goal.subtasks.some((s) => s.status === "failed" || s.status === "blocked");
  const mainSelected = !selectedSubtaskId;

  return (
    <div className={cn("flex h-full flex-col gap-3 overflow-y-auto p-3", className)}>
      {/* Overall progress — also the "main task" entry (returns ④ to the PMO
          main session). Highlighted when no subtask is selected. */}
      <button
        type="button"
        onClick={onSelectMain}
        className={cn(
          "w-full rounded-lg border bg-card p-3 text-left transition-colors",
          onSelectMain && "hover:bg-muted/60",
          mainSelected && "ring-1 ring-primary/50",
        )}
      >
        <div className="flex items-center gap-2">
          <Target className="h-4 w-4 shrink-0 text-muted-foreground" />
          <h3 className="truncate text-sm font-semibold text-foreground">
            {goal.title || t(($) => $.goal.untitled)}
          </h3>
        </div>
        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-muted">
          <div
            className={cn(
              "h-full rounded-full transition-all",
              hasFailure ? "bg-warning" : "bg-primary",
            )}
            style={{ width: `${pct}%` }}
          />
        </div>
        <p className="mt-1.5 text-xs text-muted-foreground">
          {total > 0
            ? t(($) => $.goal.progress_steps, { done, total })
            : t(($) => $.goal.progress_none)}
          {hasFailure && (
            <span className="ml-1 text-destructive">{t(($) => $.goal.has_failure)}</span>
          )}
        </p>
        {/* Coordinator (总控) attribution: which agent / runtime / model runs the
            main session (planning + summary). */}
        {goal.coordinator_name && (
          <p className="mt-1 truncate text-xs text-muted-foreground/70">
            {t(($) => $.goal.coordinator_label)}: {goal.coordinator_name}
            {[goal.coordinator_runtime_name, goal.coordinator_model]
              .filter(Boolean)
              .map((s) => ` · ${s}`)
              .join("")}
          </p>
        )}
      </button>

      {/* PMO role tree */}
      <section className="rounded-lg border bg-card p-2">
        <h4 className="px-2 py-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {t(($) => $.goal.decomposition)}
        </h4>
        <div className="flex flex-col gap-0.5">
          {goal.subtasks.length === 0 ? (
            <p className="px-2 py-3 text-center text-xs text-muted-foreground">
              {t(($) => $.goal.discussion_placeholder)}
            </p>
          ) : (
            goal.subtasks.map((subtask) => (
              <GoalSubtaskRow
                key={subtask.id}
                subtask={subtask}
                t={t}
                resolveAgentName={resolveAgentName}
                selected={selectedSubtaskId === subtask.id}
                onSelect={onSelectSubtask}
                intervene={intervene}
              />
            ))
          )}
        </div>
      </section>
    </div>
  );
}
