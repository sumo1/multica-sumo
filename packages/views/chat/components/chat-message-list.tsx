"use client";

import { useState, useRef, type ReactNode } from "react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { ChevronRight, ChevronDown, AlertTriangle, Copy } from "lucide-react";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useAutoScroll } from "@multica/ui/hooks/use-auto-scroll";
import { isTaskMessageTaskId, taskMessagesOptions } from "@multica/core/chat/queries";
import { Markdown } from "@multica/views/common/markdown";
import { copyMarkdown } from "../../editor";
import { AttachmentList } from "../../issues/components/comment-card";
import type { AgentAvailability } from "@multica/core/agents";
import type { ChatMessage, ChatPendingTask, TaskFailureReason } from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";
import { failureReasonLabel } from "../../agents/components/tabs/task-failure";
import { buildTimeline, TimelineView } from "../../common/task-transcript";
import { TaskStatusPill } from "./task-status-pill";
import { formatElapsedMs } from "../lib/format";
import { extractCopyText } from "../lib/copy-text";
import { useT } from "../../i18n";

// ─── Public component ────────────────────────────────────────────────────

interface ChatMessageListProps {
  messages: ChatMessage[];
  /**
   * Server-authoritative pending-task snapshot. `null` / undefined means
   * no in-flight task — list renders without StatusPill.
   */
  pendingTask: ChatPendingTask | null | undefined;
  /** Resolved presence; pass `undefined` while loading to keep the pill copy neutral. */
  availability: AgentAvailability | undefined;
  /** Inject non-message content at a point in the timeline. The Task page uses
   *  this to interleave the PMO planning / final-deliverable streams as
   *  in-conversation artifacts: `content` renders after the last message whose
   *  `created_at <= afterTs`, so chat sent AFTER the task completes appends
   *  below the streams, not above them (the discussion is one continuous,
   *  time-ordered thread). When `afterTs` is empty/absent, content renders at
   *  the very end (a plain footer). */
  timelineInsert?: { afterTs: string; content: ReactNode };
}

export function ChatMessageList({
  messages,
  pendingTask,
  availability,
  timelineInsert,
}: ChatMessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);
  useAutoScroll(scrollRef);

  const pendingTaskId = pendingTask?.task_id ?? null;

  // Once the assistant message for this pending task has landed in the
  // messages list, AssistantMessage owns its rendering — suppress the live
  // timeline (and pill) to avoid rendering the same content in two places
  // during the invalidate → refetch window.
  const pendingAlreadyPersisted = !!pendingTaskId && messages.some(
    (m) => m.role === "assistant" && m.task_id === pendingTaskId,
  );

  // Live timeline for the in-flight task. useRealtimeSync keeps this cache
  // current via setQueryData on task:message events.
  const showLiveTimeline = !!pendingTaskId && !pendingAlreadyPersisted;
  const canFetchLiveTimeline = isTaskMessageTaskId(pendingTaskId) && !pendingAlreadyPersisted;
  const { data: liveTaskMessages } = useQuery({
    ...taskMessagesOptions(pendingTaskId ?? ""),
    enabled: canFetchLiveTimeline,
  });
  const liveTimeline: ChatTimelineItem[] = buildTimeline(liveTaskMessages ?? []);
  const hasLive = showLiveTimeline && liveTimeline.length > 0;
  const showStatusPill = !!pendingTaskId && !pendingAlreadyPersisted && !!pendingTask;

  // Position the timeline insert (Task page planning/summary streams): it goes
  // after the last message at or before `afterTs`. Count of leading messages
  // that precede the insert; the rest render after it. With no anchor, the
  // insert falls at the end (plain footer).
  const insertAfterCount = timelineInsert?.afterTs
    ? messages.filter((m) => m.created_at <= timelineInsert.afterTs).length
    : messages.length;
  const renderInsert = (key: string) =>
    timelineInsert ? <div key={key}>{timelineInsert.content}</div> : null;

  return (
    <div
      ref={scrollRef}
      data-tab-scroll-root
      style={fadeStyle}
      className="flex-1 overflow-y-auto"
    >
      {/* Inner container matches issue / project detail width convention
       *  (max-w-4xl + mx-auto) so switching between chat and content
       *  views doesn't jolt the reading width. px-5 is a touch tighter
       *  than issue-detail's px-8 because the chat window can be narrow. */}
      <div className="mx-auto w-full max-w-4xl px-5 py-4 space-y-4">
        {messages.slice(0, insertAfterCount).map((msg) => (
          <MessageBubble
            key={msg.id}
            message={msg}
            isPending={!!pendingTaskId && msg.task_id === pendingTaskId}
          />
        ))}
        {renderInsert("timeline-insert")}
        {messages.slice(insertAfterCount).map((msg) => (
          <MessageBubble
            key={msg.id}
            message={msg}
            isPending={!!pendingTaskId && msg.task_id === pendingTaskId}
          />
        ))}
        {hasLive && (
          <div className="w-full space-y-1.5">
            <TimelineView items={liveTimeline} isStreaming />
          </div>
        )}
        {showStatusPill && pendingTask && (
          <TaskStatusPill
            pendingTask={pendingTask}
            taskMessages={liveTaskMessages ?? []}
            availability={availability}
          />
        )}
      </div>
    </div>
  );
}

/**
 * Placeholder shown while `chat_message` for a session is being fetched
 * (initial refresh, or switching to an un-cached session). Shape roughly
 * mirrors an assistant → user → assistant exchange so the window doesn't
 * shift under the user when real messages arrive.
 */
export function ChatMessageSkeleton() {
  return (
    <div className="flex-1 overflow-hidden">
      <div className="mx-auto w-full max-w-4xl px-5 py-4 space-y-5">
        <div className="space-y-2">
          <Skeleton className="h-3.5 w-3/4" />
          <Skeleton className="h-3.5 w-1/2" />
        </div>
        <div className="flex justify-end">
          <Skeleton className="h-8 w-48 rounded-2xl" />
        </div>
        <div className="space-y-2">
          <Skeleton className="h-3.5 w-2/3" />
          <Skeleton className="h-3.5 w-5/6" />
          <Skeleton className="h-3.5 w-1/3" />
        </div>
      </div>
    </div>
  );
}

// ─── Message bubbles ─────────────────────────────────────────────────────

function MessageBubble({ message, isPending }: { message: ChatMessage; isPending: boolean }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="rounded-2xl bg-muted px-3.5 py-2 text-sm max-w-[80%] break-words">
          {/* User messages are authored as markdown in ContentEditor, so
           * render them through the same pipeline as assistant replies.
           * Neutralise prose's leading/trailing margin so single-line
           * bubbles stay as compact as the plain-text version used to. */}
          <div className="prose prose-sm dark:prose-invert max-w-none [&>*:first-child]:mt-0 [&>*:last-child]:mb-0">
            <Markdown attachments={message.attachments}>{message.content}</Markdown>
          </div>
          <AttachmentList
            attachments={message.attachments}
            content={message.content}
            className="mt-1.5"
          />
        </div>
      </div>
    );
  }

  return <AssistantMessage message={message} isPending={isPending} />;
}

function AssistantMessage({
  message,
  isPending,
}: {
  message: ChatMessage;
  isPending: boolean;
}) {
  const taskId = message.task_id;
  const canFetchTaskMessages = isTaskMessageTaskId(taskId);

  // Use the shared taskMessagesOptions so this cache entry is the same one
  // seeded by useRealtimeSync during task execution — zero refetch when the
  // task finishes, since WS already populated it.
  const { data: taskMessages } = useQuery({
    ...taskMessagesOptions(taskId ?? ""),
    enabled: canFetchTaskMessages,
  });

  const timeline: ChatTimelineItem[] = buildTimeline(taskMessages ?? []);

  // Failure bubble path: when the server's FailTask wrote a failure
  // chat_message (failure_reason set), render a destructive bubble with the
  // human-readable reason label + collapsible raw errMsg + the same timeline
  // so the user can see exactly where the run broke.
  if (message.failure_reason) {
    return (
      <FailureBubble
        reason={message.failure_reason}
        rawError={message.content}
        timeline={timeline}
        elapsedMs={message.elapsed_ms}
      />
    );
  }

  return (
    <div className="w-full space-y-1.5">
      {timeline.length > 0 ? (
        <TimelineView items={timeline} attachments={message.attachments} />
      ) : (
        <div className="text-sm leading-relaxed prose prose-sm dark:prose-invert max-w-none">
          <Markdown attachments={message.attachments}>{message.content}</Markdown>
        </div>
      )}
      <AttachmentList
        attachments={message.attachments}
        content={message.content}
      />
      <MessageFooter
        message={message}
        timeline={timeline}
        isPending={isPending}
      />
    </div>
  );
}

// Inline footer row beneath the assistant reply: "Replied in 38s · [Copy]".
// Action icons live here (not as a hover-floating overlay) so they're
// discoverable on first read and don't shift content. Buttons stay quiet
// (muted) until hover. Copy is suppressed during streaming because the
// final text is still being appended.
function MessageFooter({
  message,
  timeline,
  isPending,
}: {
  message: ChatMessage;
  timeline: ChatTimelineItem[];
  isPending: boolean;
}) {
  const showCopy = !isPending;
  if (message.elapsed_ms == null && !showCopy) return null;
  return (
    <div className="flex items-center gap-1.5">
      {message.elapsed_ms != null && (
        <ElapsedCaption variant="replied" elapsedMs={message.elapsed_ms} />
      )}
      {showCopy && <MessageCopyButton message={message} timeline={timeline} />}
    </div>
  );
}

function MessageCopyButton({
  message,
  timeline,
}: {
  message: ChatMessage;
  timeline: ChatTimelineItem[];
}) {
  const { t } = useT("chat");
  const handleCopy = async () => {
    try {
      await copyMarkdown(extractCopyText(message, timeline));
      toast.success(t(($) => $.message_list.copied_toast));
    } catch {
      toast.error(t(($) => $.message_list.copy_failed_toast));
    }
  };
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground/70 hover:text-foreground"
            onClick={handleCopy}
            aria-label={t(($) => $.message_list.copy_action)}
          />
        }
      >
        <Copy />
      </TooltipTrigger>
      <TooltipContent side="top">
        {t(($) => $.message_list.copy_action)}
      </TooltipContent>
    </Tooltip>
  );
}

// Persisted "Replied in 38s" / "Failed after 12s" line under the assistant
// bubble. Reads `elapsed_ms` straight off the chat_message — server computes
// it once at task completion, so this caption is identical across reloads
// and devices. Skipped silently when null (legacy messages predating
// migration 063 + user messages).
function ElapsedCaption({
  variant,
  elapsedMs,
  className,
}: {
  variant: "replied" | "failed";
  elapsedMs: number;
  className?: string;
}) {
  const { t } = useT("chat");
  const text =
    variant === "replied"
      ? t(($) => $.message_list.replied_in, { elapsed: formatElapsedMs(elapsedMs) })
      : t(($) => $.message_list.failed_after, { elapsed: formatElapsedMs(elapsedMs) });
  return (
    <div className={cn("text-xs text-muted-foreground/80", className)}>
      {text}
    </div>
  );
}

function FailureBubble({
  reason,
  rawError,
  timeline,
  elapsedMs,
}: {
  reason: string;
  rawError: string;
  timeline: ChatTimelineItem[];
  elapsedMs?: number | null;
}) {
  const { t } = useT("chat");
  const [open, setOpen] = useState(false);
  // Map the back-end enum to copy via the shared label table; an unknown
  // reason (e.g. a future enum value the front-end doesn't ship yet)
  // falls back to a generic translated label.
  const label =
    failureReasonLabel[reason as TaskFailureReason] ??
    t(($) => $.message_list.task_failed_fallback);

  return (
    <div className="w-full space-y-1.5">
      {/* Failure read as an inline, low-key note — not a destructive
       *  alert. Intentionally borderless / no background tint: a chat
       *  failure is informational ("this didn't work"), not a system
       *  error. The icon + muted destructive text are signal enough,
       *  the rest stays in the normal reply rhythm. */}
      <div className="flex items-start gap-1.5 text-sm">
        <AlertTriangle className="size-3.5 shrink-0 text-destructive/80 mt-0.5" />
        <div className="flex-1 min-w-0">
          <div className="text-destructive/90">{label}</div>
          {rawError.trim() && (
            <Collapsible open={open} onOpenChange={setOpen}>
              <CollapsibleTrigger className="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors">
                {open ? (
                  <ChevronDown className="size-3" />
                ) : (
                  <ChevronRight className="size-3" />
                )}
                <span>{t(($) => $.message_list.show_details)}</span>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <pre className="mt-1 max-h-40 overflow-auto rounded bg-muted/40 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-all">
                  {rawError}
                </pre>
              </CollapsibleContent>
            </Collapsible>
          )}
        </div>
      </div>
      {timeline.length > 0 && <TimelineView items={timeline} />}
      {elapsedMs != null && (
        <ElapsedCaption variant="failed" elapsedMs={elapsedMs} />
      )}
    </div>
  );
}

// ─── Shared ──────────────────────────────────────────────────────────────
