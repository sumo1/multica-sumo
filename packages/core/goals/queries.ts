import { queryOptions } from "@tanstack/react-query";

import { api } from "../api";

export const goalKeys = {
  all: (wsId: string) => ["goals", wsId] as const,
  /** A single goal_run plus its subtasks. */
  run: (wsId: string, id: string) => [...goalKeys.all(wsId), "run", id] as const,
  /** The workspace's task list (Task page ② column). */
  list: (wsId: string) => [...goalKeys.all(wsId), "list"] as const,
};

/** The workspace's tasks (goals), newest first. Drives the Task page list. */
export function taskListOptions(wsId: string) {
  return queryOptions({
    queryKey: goalKeys.list(wsId),
    queryFn: () => api.listTasks(),
    enabled: !!wsId,
  });
}

/**
 * One goal_run with its subtask DAG. The status tree reads this; WS
 * goal:* events invalidate it (see use-realtime-sync.ts). Keyed on wsId so a
 * workspace switch swaps the cache automatically.
 */
export function goalRunOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: goalKeys.run(wsId, id),
    queryFn: () => api.getGoal(id),
    enabled: !!wsId && !!id,
  });
}
