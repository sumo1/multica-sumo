"use client";

import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import { buildTimeline, TimelineView } from "../../common/task-transcript";

/**
 * A task's execution transcript (thinking / tool calls / final answer) for the
 * Task page ④ column — both the PMO main session and per-subtask output.
 *
 * Data-only wrapper: it fetches the task_messages for `taskId` and hands them to
 * the shared <TimelineView>, the same conductor-style renderer the chat list
 * uses. That gives thinking-vs-answer separation, a collapsible process fold,
 * and full markdown for free. Live updates ride the global `task:message` WS
 * handler, which writes the same ["task-messages", taskId] cache this reads.
 */
export function TaskStream({
  taskId,
  running,
  emptyHint,
}: {
  taskId: string;
  running?: boolean;
  emptyHint?: string;
}) {
  const { data: messages } = useQuery({
    ...taskMessagesOptions(taskId),
    enabled: !!taskId,
  });

  const items = buildTimeline(messages ?? []);

  if (items.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        {running ? (
          <span className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            {emptyHint}
          </span>
        ) : (
          <span className="text-sm text-muted-foreground">{emptyHint}</span>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-1.5 p-3">
      <TimelineView items={items} isStreaming={running} />
      {running && (
        <span className="flex items-center gap-2 px-1 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
        </span>
      )}
    </div>
  );
}
