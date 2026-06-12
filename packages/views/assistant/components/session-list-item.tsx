"use client";

import React, { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { pendingChatTaskOptions } from "@multica/core/chat/queries";
import type { Agent, ChatSession, RuntimeDevice } from "@multica/core/types";

interface SessionListItemProps {
  session: ChatSession;
  agent: Agent | undefined;
  /** Runtime this session is bound to. Undefined for legacy sessions with no binding. */
  runtime: RuntimeDevice | undefined;
  isActive: boolean;
  onClick: () => void;
}

export const SessionListItem = React.memo(function SessionListItem({
  session,
  agent,
  runtime,
  isActive,
  onClick,
}: SessionListItemProps) {
  // 获取该会话的运行状态
  const { data: pendingTask } = useQuery(pendingChatTaskOptions(session.id));
  const isRunning = !!pendingTask?.task_id;

  const title = session.title?.trim() || "无标题";

  return (
    <div
      onClick={onClick}
      className={cn(
        "group relative flex min-h-14 cursor-pointer flex-col gap-1 rounded-lg border px-3 py-2 transition-colors hover:bg-accent/60",
        isActive && "bg-accent border-brand",
        !isActive && "border-transparent"
      )}
    >
      {/* 第一行：Agent 头像 + 名称 + 状态 */}
      <div className="flex items-center gap-2">
        {agent && (
          <ActorAvatar
            actorType="agent"
            actorId={agent.id}
            size={20}
            showStatusDot
          />
        )}
        <span className="text-xs font-medium truncate flex-1">{agent?.name || "未知 Agent"}</span>
        {isRunning && pendingTask?.created_at ? (
          <RunningDuration startedAt={pendingTask.created_at} />
        ) : session.has_unread ? (
          <span className="size-1.5 rounded-full bg-brand shrink-0" />
        ) : null}
      </div>

      {/* 第二行：会话标题 */}
      <div className="text-sm text-muted-foreground truncate">{title}</div>

      {/* 第三行：绑定的运行时（Claude Code / Codex …）+ 在线状态 */}
      {runtime && (
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <ProviderLogo provider={runtime.provider} className="size-3 shrink-0" />
          <span className="truncate">{runtime.name}</span>
          <span
            className={cn(
              "size-1.5 rounded-full shrink-0",
              runtime.status === "online" ? "bg-emerald-500" : "bg-muted-foreground/40"
            )}
            title={runtime.status === "online" ? "在线" : "离线"}
          />
        </div>
      )}
    </div>
  );
});

/**
 * 实时运行时长显示组件
 * 每秒更新一次
 */
function RunningDuration({ startedAt }: { startedAt: string }) {
  const [elapsed, setElapsed] = useState(0);

  useEffect(() => {
    const start = new Date(startedAt).getTime();

    // 立即计算一次
    const updateElapsed = () => {
      const now = Date.now();
      setElapsed(Math.floor((now - start) / 1000));
    };

    updateElapsed();

    // 每秒更新
    const timer = setInterval(updateElapsed, 1000);

    return () => clearInterval(timer);
  }, [startedAt]);

  return (
    <div className="flex items-center gap-1 shrink-0 text-xs font-medium text-emerald-600">
      <Loader2 className="size-3 animate-spin" />
      <span>{formatDuration(elapsed)}</span>
    </div>
  );
}

/**
 * 格式化时长为 "2m 15s" 格式
 */
function formatDuration(seconds: number): string {
  if (seconds < 60) {
    return `${seconds}s`;
  }

  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;

  if (minutes < 60) {
    return `${minutes}m ${remainingSeconds}s`;
  }

  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;

  return `${hours}h ${remainingMinutes}m`;
}
