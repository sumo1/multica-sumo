"use client";

import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import { RuntimePicker } from "../../agents/components/runtime-picker";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import type { Agent, MemberWithUser, RuntimeDevice } from "@multica/core/types";

export function NewSessionDialog({
  open,
  onOpenChange,
  agents,
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  onCreateSession,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agents: Agent[];
  runtimes: RuntimeDevice[];
  runtimesLoading: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  onCreateSession: (agentId: string, runtimeId: string) => Promise<void>;
}) {
  const { t } = useT("chat");
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [selectedRuntimeId, setSelectedRuntimeId] = useState("");
  const [isCreating, setIsCreating] = useState(false);

  // 当对话框打开时，重置选择
  useEffect(() => {
    if (open) {
      // 默认选择第一个可用的 agent
      const firstAgent = agents.find((a) => !a.archived_at);
      setSelectedAgentId(firstAgent?.id ?? "");
      setSelectedRuntimeId("");
    }
  }, [open, agents]);

  const handleCreate = async () => {
    if (!selectedAgentId || !selectedRuntimeId) return;

    setIsCreating(true);
    try {
      await onCreateSession(selectedAgentId, selectedRuntimeId);
    } finally {
      setIsCreating(false);
    }
  };

  // A chat session binds its runtime immutably at create time, so an offline
  // runtime would produce a conversation that can never run. Block create when
  // the selected runtime is not online (the picker also disables offline rows).
  const selectedRuntime = runtimes.find((r) => r.id === selectedRuntimeId);
  const runtimeOnline = selectedRuntime?.status === "online";
  const canCreate =
    !!selectedAgentId && !!selectedRuntimeId && runtimeOnline && !isCreating;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[500px]">
        <DialogHeader>
          <DialogTitle>{t(($) => $.new_dialog.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.new_dialog.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6 py-4">
          {/* 智能体选择 */}
          <div className="space-y-2">
            <Label>{t(($) => $.new_dialog.agent_label)}</Label>
            <div className="space-y-2">
              {agents.filter((a) => !a.archived_at).map((agent) => (
                <button
                  key={agent.id}
                  type="button"
                  onClick={() => setSelectedAgentId(agent.id)}
                  className={`flex w-full items-center gap-3 rounded-lg border px-4 py-3 text-left transition-colors ${
                    selectedAgentId === agent.id
                      ? "border-primary bg-accent"
                      : "border-border hover:bg-accent/50"
                  }`}
                >
                  <ActorAvatar
                    actorType="agent"
                    actorId={agent.id}
                    size={32}
                  />
                  <div className="flex-1 min-w-0">
                    <div className="font-medium truncate">{agent.name}</div>
                    {agent.instructions && (
                      <div className="text-xs text-muted-foreground truncate">
                        {agent.instructions}
                      </div>
                    )}
                  </div>
                  {selectedAgentId === agent.id && (
                    <div className="h-2 w-2 rounded-full bg-primary" />
                  )}
                </button>
              ))}
              {agents.filter((a) => !a.archived_at).length === 0 && (
                <div className="text-sm text-muted-foreground text-center py-8">
                  {t(($) => $.new_dialog.no_agents)}
                </div>
              )}
            </div>
          </div>

          {/* 运行时选择 */}
          {selectedAgentId && (
            <RuntimePicker
              runtimes={runtimes}
              runtimesLoading={runtimesLoading}
              members={members}
              currentUserId={currentUserId}
              selectedRuntimeId={selectedRuntimeId}
              onSelect={setSelectedRuntimeId}
              blockOffline
            />
          )}
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={isCreating}
          >
            {t(($) => $.new_dialog.cancel)}
          </Button>
          <Button
            onClick={handleCreate}
            disabled={!canCreate}
          >
            {isCreating ? t(($) => $.new_dialog.creating) : t(($) => $.new_dialog.create)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
