"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2, Target } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { squadListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { goalRunOptions } from "@multica/core/goals/queries";
import { useCreateGoal, useGoalIntervention } from "@multica/core/goals/mutations";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";
import { GoalStatusTree } from "./goal-status-tree";

// ---------------------------------------------------------------------------
// GoalPanel — the user-facing entry to goal mode. Pick a team (squad), describe
// a goal, and hand it to the PMO for LLM decomposition + execution. The live
// status tree (right) is fed by goalRunOptions, kept fresh by goal:* WS events
// (use-realtime-sync invalidates ["goals", wsId]).
// ---------------------------------------------------------------------------

export interface GoalPanelProps {
  className?: string;
  /** Called with the new chat session id when the user takes over a failed
   * subtask, so the host (assistant page) can switch to that chat. */
  onTakeover?: (chatSessionId: string) => void;
}

export function GoalPanel({ className, onTakeover }: GoalPanelProps) {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();

  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const createGoal = useCreateGoal();
  const { retry, reassign, editSpec, skip, takeover } = useGoalIntervention();

  const [squadId, setSquadId] = useState<string>("");
  const [goalText, setGoalText] = useState("");
  // The goal we created this session — drives the live status tree.
  const [activeGoalId, setActiveGoalId] = useState<string | null>(null);
  // Subtask currently being reassigned (shows the agent picker overlay).
  const [reassigningId, setReassigningId] = useState<string | null>(null);

  // Live goal_run + subtasks. Disabled until a goal exists.
  const { data: goal } = useQuery({
    ...goalRunOptions(wsId, activeGoalId ?? ""),
    enabled: !!wsId && !!activeGoalId,
    // While the PMO is planning the subtasks land asynchronously (after the
    // planning agent submits), so poll until the goal leaves 'planning'.
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "planning" || status === "executing" ? 3000 : false;
    },
  });

  const resolveAgentName = useMemo(() => {
    const byId = new Map(agents.map((a) => [a.id, a.name]));
    return (id: string) => byId.get(id);
  }, [agents]);

  // Which subtask currently has an in-flight intervention (to disable its
  // buttons). Each mutation's `variables` is the id (retry/skip) or carries it.
  let busySubtaskId: string | null = null;
  if (retry.isPending && typeof retry.variables === "string") busySubtaskId = retry.variables;
  else if (skip.isPending && typeof skip.variables === "string") busySubtaskId = skip.variables;
  else if (takeover.isPending && typeof takeover.variables === "string") busySubtaskId = takeover.variables;
  else if (reassign.isPending && reassign.variables) busySubtaskId = reassign.variables.subtaskId;
  else if (editSpec.isPending && editSpec.variables) busySubtaskId = editSpec.variables.subtaskId;

  const interveneHandlers = {
    onRetry: (subtaskId: string) => retry.mutate(subtaskId),
    onReassign: (subtaskId: string) => setReassigningId(subtaskId),
    onEditSpec: (subtaskId: string, spec: string) => editSpec.mutate({ subtaskId, spec }),
    onSkip: (subtaskId: string) => skip.mutate(subtaskId),
    onTakeover: (subtaskId: string) =>
      takeover.mutate(subtaskId, {
        onSuccess: (session) => {
          if (session?.id) onTakeover?.(session.id);
        },
      }),
    busySubtaskId,
  };

  const canSubmit =
    !!squadId && goalText.trim().length > 0 && !createGoal.isPending;

  const handleSubmit = async () => {
    if (!canSubmit) return;
    try {
      const run = await createGoal.mutateAsync({
        squad_id: squadId,
        title: goalText.trim().slice(0, 80),
        goal: goalText.trim(),
        auto_decompose: true,
      });
      setActiveGoalId(run.id);
      setGoalText("");
    } catch {
      // Mutation logs the error; surfaced inline below via isError.
    }
  };

  return (
    <div className={cn("relative", className)}>
      <div className="flex h-full">
        {/* Left: goal composer */}
        <div className="flex w-[360px] shrink-0 flex-col gap-4 border-r p-4">
          <div className="flex items-center gap-2">
            <Target className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">{t(($) => $.goal.panel.title)}</h2>
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-muted-foreground">
              {t(($) => $.goal.panel.squad_label)}
            </label>
            {squads.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.goal.panel.no_squads)}
              </p>
            ) : (
              <Select value={squadId} onValueChange={(v) => v && setSquadId(v)}>
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t(($) => $.goal.panel.squad_placeholder)} />
                </SelectTrigger>
                <SelectContent>
                  {squads.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>

          <div className="flex flex-1 flex-col gap-1.5">
            <label className="text-xs font-medium text-muted-foreground">
              {t(($) => $.goal.panel.goal_label)}
            </label>
            <Textarea
              value={goalText}
              onChange={(e) => setGoalText(e.target.value)}
              placeholder={t(($) => $.goal.panel.goal_placeholder)}
              className="min-h-[120px] flex-1 resize-none"
            />
          </div>

          {createGoal.isError && (
            <p className="text-xs text-destructive">
              {t(($) => $.goal.panel.submit_error)}
            </p>
          )}

          <Button onClick={handleSubmit} disabled={!canSubmit} className="w-full">
            {createGoal.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {t(($) => $.goal.panel.submit)}
          </Button>
        </div>

        {/* Right: live status tree */}
        <div className="flex-1 overflow-hidden">
          {goal && goal.id ? (
            <GoalStatusTree
              goal={goal}
              resolveAgentName={resolveAgentName}
              intervene={interveneHandlers}
            />
          ) : (
            <div className="flex h-full items-center justify-center p-6">
              <p className="max-w-xs text-center text-sm text-muted-foreground">
                {activeGoalId
                  ? t(($) => $.goal.panel.planning)
                  : t(($) => $.goal.panel.empty)}
              </p>
            </div>
          )}
        </div>
      </div>

      {/* Reassign agent picker overlay */}
      {reassigningId && (
        <div
          className="absolute inset-0 z-10 flex items-center justify-center bg-black/30"
          onClick={() => setReassigningId(null)}
        >
          <div
            className="w-72 rounded-lg border bg-card p-4 shadow-lg"
            onClick={(e) => e.stopPropagation()}
          >
            <label className="mb-1.5 block text-xs font-medium text-muted-foreground">
              {t(($) => $.goal.intervene.reassign_placeholder)}
            </label>
            <Select
              onValueChange={(value) => {
                const agentId = String(value ?? "");
                if (agentId && reassigningId) {
                  reassign.mutate({ subtaskId: reassigningId, agentId });
                  setReassigningId(null);
                }
              }}
            >
              <SelectTrigger className="w-full">
                <SelectValue placeholder={t(($) => $.goal.panel.squad_placeholder)} />
              </SelectTrigger>
              <SelectContent>
                {agents
                  .filter((a) => !a.archived_at)
                  .map((a) => (
                    <SelectItem key={a.id} value={a.id}>
                      {a.name}
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
          </div>
        </div>
      )}
    </div>
  );
}
