import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { GoalRun, GoalSubtask } from "@multica/core/types";
import { renderWithI18n as render } from "../../test/i18n";
import { GoalStatusTree } from "./goal-status-tree";

function subtask(over: Partial<GoalSubtask>): GoalSubtask {
  return {
    id: "st",
    goal_run_id: "gr",
    seq: 0,
    title: "Subtask",
    spec: "",
    assignee_agent_id: "",
    depends_on: [],
    status: "pending",
    kind: "execute",
    verdict: "",
    attempt: 0,
    max_attempts: 2,
    failure_reason: "",
    task_id: "",
    agent_name: "",
    runtime_name: "",
    runtime_provider: "",
    model: "",
    upstream_output: "",
    handoff_brief: "",
    ...over,
  };
}

function goal(over: Partial<GoalRun>): GoalRun {
  return {
    id: "gr",
    workspace_id: "ws",
    squad_id: "sq",
    chat_session_id: "",
    title: "Refactor login",
    goal: "do it",
    status: "executing",
    subtasks: [],
    planning_task_id: "",
    summary_task_id: "",
    confirmed_at: "",
    project_id: "",
    persist_task_id: "",
    can_persist: false,
    coordinator_name: "",
    coordinator_runtime_name: "",
    coordinator_runtime_provider: "",
    coordinator_model: "",
    created_at: "",
    updated_at: "",
    ...over,
  };
}

describe("GoalStatusTree", () => {
  it("renders the goal title and progress fraction", () => {
    render(
      <GoalStatusTree
        goal={goal({
          subtasks: [
            subtask({ id: "a", seq: 1, title: "API", status: "completed" }),
            subtask({ id: "b", seq: 2, title: "UI", status: "running" }),
          ],
        })}
      />,
    );
    expect(screen.getByText("Refactor login")).toBeInTheDocument();
    expect(screen.getByText("1/2 steps")).toBeInTheDocument();
  });

  it("shows the discussion placeholder when there are no subtasks", () => {
    render(<GoalStatusTree goal={goal({ subtasks: [] })} />);
    expect(screen.getByText("In discussion, no subtasks yet")).toBeInTheDocument();
  });

  it("renders the failure reason and retry count on a failed subtask", () => {
    render(
      <GoalStatusTree
        goal={goal({
          status: "partial",
          subtasks: [
            subtask({
              id: "a",
              seq: 1,
              title: "Flaky",
              status: "failed",
              failure_reason: "lint error",
              attempt: 2,
              max_attempts: 2,
            }),
          ],
        })}
      />,
    );
    expect(screen.getByText(/lint error/)).toBeInTheDocument();
    expect(screen.getByText(/retried 2\/2/)).toBeInTheDocument();
    // partial/failed goal shows the warning marker
    expect(screen.getByText(/has failures\/blocks/)).toBeInTheDocument();
  });

  it("resolves the assignee agent name and fires onSelectSubtask", async () => {
    const onSelect = vi.fn();
    render(
      <GoalStatusTree
        goal={goal({
          subtasks: [subtask({ id: "a", seq: 1, title: "API", assignee_agent_id: "agent-123" })],
        })}
        resolveAgentName={(id) => (id === "agent-123" ? "Architect" : undefined)}
        onSelectSubtask={onSelect}
      />,
    );
    expect(screen.getByText("Architect")).toBeInTheDocument();
    await userEvent.click(screen.getByText("API"));
    expect(onSelect).toHaveBeenCalledWith("a");
  });

  it("shows runtime · model attribution on a subtask, and the coordinator label", () => {
    render(
      <GoalStatusTree
        goal={goal({
          coordinator_name: "PMO",
          coordinator_runtime_name: "Cloud",
          coordinator_model: "gpt-5",
          subtasks: [
            subtask({
              id: "a",
              seq: 1,
              title: "API",
              agent_name: "coder",
              runtime_name: "Local",
              model: "claude-opus-4-8",
            }),
          ],
        })}
      />,
    );
    // Subtask row: prefers the API-resolved agent_name + appends runtime · model.
    expect(screen.getByText("coder", { exact: false })).toBeInTheDocument();
    expect(screen.getByText(/Local · claude-opus-4-8/)).toBeInTheDocument();
    // Coordinator attribution under the overall-progress header.
    expect(screen.getByText(/Coordinator: PMO · Cloud · gpt-5/)).toBeInTheDocument();
  });

  it("downgrades an unknown subtask status to the pending icon without crashing", () => {
    render(
      <GoalStatusTree
        goal={goal({
          // Simulate enum drift: a status the UI doesn't know yet.
          subtasks: [subtask({ id: "a", seq: 1, title: "Mystery", status: "teleporting" as never })],
        })}
      />,
    );
    expect(screen.getByText("Mystery")).toBeInTheDocument();
  });

  it("marks a verify node and shows its pass verdict", () => {
    render(
      <GoalStatusTree
        goal={goal({
          subtasks: [
            subtask({ id: "a", seq: 1, title: "Build", status: "completed" }),
            subtask({
              id: "v",
              seq: 2,
              title: "Security review",
              kind: "verify",
              verdict: "pass",
              status: "completed",
              depends_on: ["a"],
            }),
          ],
        })}
      />,
    );
    expect(screen.getByText("Security review")).toBeInTheDocument();
    // The verify node surfaces its verdict badge (English locale).
    expect(screen.getByText("pass")).toBeInTheDocument();
    // The shield affordance is labelled for the verify node.
    expect(screen.getByLabelText("Adversarial review")).toBeInTheDocument();
  });

  it("shows a reject verdict on a verify node", () => {
    render(
      <GoalStatusTree
        goal={goal({
          subtasks: [
            subtask({
              id: "v",
              seq: 1,
              title: "Review",
              kind: "verify",
              verdict: "reject",
              status: "completed",
            }),
          ],
        })}
      />,
    );
    expect(screen.getByText("reject")).toBeInTheDocument();
  });

  it("shows intervention buttons on a failed node and fires retry/skip", async () => {
    const onRetry = vi.fn();
    const onSkip = vi.fn();
    render(
      <GoalStatusTree
        goal={goal({
          status: "failed",
          subtasks: [
            subtask({ id: "a", seq: 1, title: "Flaky", status: "failed", failure_reason: "boom" }),
          ],
        })}
        intervene={{ onRetry, onSkip }}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(onRetry).toHaveBeenCalledWith("a");
    await userEvent.click(screen.getByRole("button", { name: "Skip" }));
    expect(onSkip).toHaveBeenCalledWith("a");
  });

  it("does NOT show intervention buttons on a running node", () => {
    const onRetry = vi.fn();
    render(
      <GoalStatusTree
        goal={goal({
          subtasks: [subtask({ id: "a", seq: 1, title: "Working", status: "running" })],
        })}
        intervene={{ onRetry }}
      />,
    );
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  });

  it("edit-spec opens an inline editor and submits the new spec", async () => {
    const onEditSpec = vi.fn();
    render(
      <GoalStatusTree
        goal={goal({
          status: "failed",
          subtasks: [
            subtask({ id: "a", seq: 1, title: "Flaky", spec: "old spec", status: "failed" }),
          ],
        })}
        intervene={{ onEditSpec }}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Edit spec" }));
    const textarea = screen.getByPlaceholderText("Edit the spec, then retry…");
    await userEvent.clear(textarea);
    await userEvent.type(textarea, "new spec");
    await userEvent.click(screen.getByRole("button", { name: "Save & retry" }));
    expect(onEditSpec).toHaveBeenCalledWith("a", "new spec");
  });

  it("shows a takeover button on a failed node and fires onTakeover", async () => {
    const onTakeover = vi.fn();
    render(
      <GoalStatusTree
        goal={goal({
          status: "failed",
          subtasks: [subtask({ id: "a", seq: 1, title: "Flaky", status: "failed" })],
        })}
        intervene={{ onTakeover }}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Take over" }));
    expect(onTakeover).toHaveBeenCalledWith("a");
  });

  it("disables buttons for the busy subtask", () => {
    render(
      <GoalStatusTree
        goal={goal({
          status: "failed",
          subtasks: [subtask({ id: "a", seq: 1, title: "Flaky", status: "failed" })],
        })}
        intervene={{ onRetry: vi.fn(), busySubtaskId: "a" }}
      />,
    );
    // Busy buttons show the working label and are disabled.
    const btn = screen.getByRole("button", { name: "Working…" });
    expect(btn).toBeDisabled();
  });
});
