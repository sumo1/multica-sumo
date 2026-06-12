// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, cleanup, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enChat from "../../locales/en/chat.json";
import type { GoalRun } from "@multica/core/types";

const TEST_RESOURCES = { en: { chat: enChat } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const { mockCreateGoal, mockGetGoal, mockListSquads, mockListAgents } = vi.hoisted(() => ({
  mockCreateGoal: vi.fn(),
  mockGetGoal: vi.fn(),
  mockListSquads: vi.fn(),
  mockListAgents: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    createGoal: mockCreateGoal,
    getGoal: mockGetGoal,
    listSquads: mockListSquads,
    listAgents: mockListAgents,
  },
}));

import { GoalPanel } from "./goal-panel";

const PLANNING_GOAL: GoalRun = {
  id: "goal-1",
  workspace_id: "ws-1",
  squad_id: "sq-1",
  chat_session_id: "",
  title: "Refactor login",
  goal: "Refactor the login module",
  status: "planning",
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
};

const EXECUTING_GOAL: GoalRun = {
  ...PLANNING_GOAL,
  status: "executing",
  subtasks: [
    {
      id: "st-1",
      goal_run_id: "goal-1",
      seq: 1,
      title: "Backend API",
      spec: "build it",
      assignee_agent_id: "agent-1",
      depends_on: [],
      status: "running",
      kind: "execute",
      verdict: "",
      attempt: 1,
      max_attempts: 2,
      failure_reason: "",
      task_id: "",
      agent_name: "",
      runtime_name: "",
      runtime_provider: "",
      model: "",
      upstream_output: "",
      handoff_brief: "",
    },
  ],
};

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <GoalPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  mockListSquads.mockResolvedValue([
    { id: "sq-1", name: "Backend Squad", workspace_id: "ws-1", leader_id: "agent-1" },
  ]);
  mockListAgents.mockResolvedValue([{ id: "agent-1", name: "Architect", workspace_id: "ws-1" }]);
  mockCreateGoal.mockResolvedValue(PLANNING_GOAL);
  mockGetGoal.mockResolvedValue(EXECUTING_GOAL);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("GoalPanel", () => {
  it("shows the empty prompt before a goal is started", async () => {
    renderPanel();
    expect(
      await screen.findByText("Pick a team and describe a goal to get started."),
    ).toBeInTheDocument();
  });

  it("creates a goal with auto_decompose and renders the live status tree", async () => {
    renderPanel();

    // Wait for squads to load and the trigger to render.
    const trigger = await screen.findByRole("combobox");
    await userEvent.click(trigger);
    await userEvent.click(await screen.findByText("Backend Squad"));

    const textarea = screen.getByPlaceholderText(
      "Describe the goal for the team to plan and execute…",
    );
    await userEvent.type(textarea, "Refactor the login module");

    await userEvent.click(screen.getByRole("button", { name: /Start goal/ }));

    await waitFor(() => {
      expect(mockCreateGoal).toHaveBeenCalledWith(
        expect.objectContaining({
          squad_id: "sq-1",
          goal: "Refactor the login module",
          auto_decompose: true,
        }),
      );
    });

    // The live query (getGoal) returns the executing goal → tree renders the
    // subtask with its resolved role name.
    expect(await screen.findByText("Refactor login")).toBeInTheDocument();
    expect(await screen.findByText("Backend API")).toBeInTheDocument();
    expect(await screen.findByText("Architect")).toBeInTheDocument();
  });

  it("shows the no-teams hint when the workspace has no squads", async () => {
    mockListSquads.mockResolvedValue([]);
    renderPanel();
    expect(
      await screen.findByText("No teams yet — create one in Squads first."),
    ).toBeInTheDocument();
  });
});
