import { describe, expect, it } from "vitest";
import {
  ChatSessionSchema,
  ChatSessionListSchema,
  DashboardAgentRunTimeListSchema,
  DashboardUsageByAgentListSchema,
  DashboardUsageDailyListSchema,
  DuplicateIssueErrorBodySchema,
  EMPTY_CHAT_SESSION,
  EMPTY_USER,
  GoalRunSchema,
  GoalSubtaskSchema,
  EMPTY_GOAL_RUN,
  ListIssuesResponseSchema,
  RuntimeHourlyActivityListSchema,
  RuntimeUsageByAgentListSchema,
  RuntimeUsageByHourListSchema,
  RuntimeUsageListSchema,
  SquadListSchema,
  SquadSchema,
  UserSchema,
} from "./schemas";
import { parseWithFallback } from "./schema";

const baseIssue = {
  id: "11111111-1111-1111-1111-111111111111",
  workspace_id: "ws-1",
  number: 1,
  identifier: "MUL-1",
  title: "Test",
  description: null,
  status: "todo",
  priority: "medium",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  start_date: null,
  due_date: null,
  metadata: {},
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

describe("IssueSchema (via ListIssuesResponseSchema)", () => {
  it("accepts a primitive metadata KV map", () => {
    const payload = {
      issues: [
        {
          ...baseIssue,
          metadata: { pipeline_status: "waiting", pr_number: 3, is_blocked: true },
        },
      ],
      total: 1,
    };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.metadata).toEqual({
      pipeline_status: "waiting",
      pr_number: 3,
      is_blocked: true,
    });
  });

  it("defaults metadata to {} when the server omits it (older backend)", () => {
    const { metadata: _omit, ...issueWithoutMetadata } = baseIssue;
    const payload = { issues: [issueWithoutMetadata], total: 1 };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.metadata).toEqual({});
  });

  it("rejects metadata with non-primitive values (nested object)", () => {
    const payload = {
      issues: [{ ...baseIssue, metadata: { nested: { x: 1 } } }],
      total: 1,
    };
    expect(ListIssuesResponseSchema.safeParse(payload).success).toBe(false);
  });
});

// The duplicate-issue branch in create-issue.tsx feeds ApiError.body
// (typed as `unknown`) through this schema. Any future server drift that
// loses the contract MUST fail the parse so the UI falls back to a normal
// error toast instead of rendering an empty / partial duplicate card.
describe("DuplicateIssueErrorBodySchema", () => {
  const valid = {
    code: "active_duplicate_issue",
    error: "An active issue with this title already exists: MUL-12 – Login bug",
    issue: {
      id: "11111111-1111-1111-1111-111111111111",
      identifier: "MUL-12",
      title: "Login bug",
    },
  };

  it("accepts a well-formed body", () => {
    expect(DuplicateIssueErrorBodySchema.safeParse(valid).success).toBe(true);
  });

  it("accepts unknown extra fields via .loose()", () => {
    const forwardCompat = {
      ...valid,
      hint: "Try a different title",
      issue: { ...valid.issue, workspace_id: "ws-1", status: "todo" },
    };
    expect(DuplicateIssueErrorBodySchema.safeParse(forwardCompat).success).toBe(true);
  });

  it("rejects a renamed code (so renames degrade to the generic toast)", () => {
    const renamed = { ...valid, code: "duplicate_issue" };
    expect(DuplicateIssueErrorBodySchema.safeParse(renamed).success).toBe(false);
  });

  it("rejects a missing issue object", () => {
    const { issue: _omit, ...without } = valid;
    expect(DuplicateIssueErrorBodySchema.safeParse(without).success).toBe(false);
  });

  it("rejects a non-string issue.id", () => {
    const broken = { ...valid, issue: { ...valid.issue, id: 42 } };
    expect(DuplicateIssueErrorBodySchema.safeParse(broken).success).toBe(false);
  });

  it("accepts a missing error field (it is optional)", () => {
    const { error: _omit, ...without } = valid;
    expect(DuplicateIssueErrorBodySchema.safeParse(without).success).toBe(true);
  });
});

// `user.timezone` (Viewing tz) was added in the timezone-architecture RFC.
// A desktop build older than the server — or a server predating the
// `user.timezone` migration — will return a `/api/me` body with no
// `timezone` key. The schema must not fail closed on that: the field
// defaults to `null`, which the frontend resolves to the browser-detected
// tz at render time.
describe("UserSchema timezone drift", () => {
  const base = {
    id: "11111111-1111-1111-1111-111111111111",
    name: "Ada",
    email: "ada@example.com",
  };

  it("defaults timezone to null when the field is absent", () => {
    const parsed = UserSchema.parse(base);
    expect(parsed.timezone).toBe(null);
  });

  it("preserves an explicit IANA timezone", () => {
    const parsed = UserSchema.parse({ ...base, timezone: "Asia/Tokyo" });
    expect(parsed.timezone).toBe("Asia/Tokyo");
  });

  it("accepts an explicit null timezone", () => {
    const parsed = UserSchema.parse({ ...base, timezone: null });
    expect(parsed.timezone).toBe(null);
  });

  // Wrong-type drift: a future server bug sending `timezone` as a number
  // must not throw into the UI. parseWithFallback degrades the whole user
  // object to the explicit fallback (EMPTY_USER) so /api/me callers keep a
  // valid shape instead of white-screening.
  it("falls back to EMPTY_USER when timezone is the wrong type", () => {
    const parsed = parseWithFallback(
      { ...base, timezone: 42 },
      UserSchema,
      EMPTY_USER,
      { endpoint: "GET /api/me" },
    );
    expect(parsed).toBe(EMPTY_USER);
  });
});

describe("SquadListSchema member preview drift", () => {
  const baseSquad = {
    id: "squad-1",
    workspace_id: "ws-1",
    name: "Frontend Squad",
    description: "",
    instructions: "",
    avatar_url: null,
    leader_id: "agent-1",
    creator_id: "user-1",
    created_at: "2026-05-01T00:00:00Z",
    updated_at: "2026-05-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
  };

  it("defaults preview fields when an older backend omits them", () => {
    const parsed = SquadListSchema.parse([baseSquad]);
    expect(parsed[0]?.member_count).toBe(0);
    expect(parsed[0]?.member_preview).toEqual([]);
  });

  it("defaults preview fields on a single squad response", () => {
    const parsed = SquadSchema.parse(baseSquad);
    expect(parsed.member_count).toBe(0);
    expect(parsed.member_preview).toEqual([]);
  });

  it("preserves lightweight member preview rows", () => {
    const parsed = SquadListSchema.parse([
      {
        ...baseSquad,
        member_count: 2,
        member_preview: [
          { member_type: "agent", member_id: "agent-1", role: "leader" },
          { member_type: "member", member_id: "user-2", role: "member" },
        ],
      },
    ]);
    expect(parsed[0]?.member_count).toBe(2);
    expect(parsed[0]?.member_preview).toHaveLength(2);
    expect(parsed[0]?.member_preview?.[0]?.role).toBe("leader");
  });
});

// The workspace dashboard and runtime-detail pages were re-pointed at the
// unified `task_usage_hourly` rollup. Every numeric field drives chart /
// KPI math, and string keys (date / agent_id / model) bucket the series.
// The contract these schemas must hold: a row missing a field degrades
// that field to a sane default rather than dropping the WHOLE array to
// the `[]` fallback — one drifted row must not blank the entire chart.
describe("dashboard + runtime usage schema drift", () => {
  it("coerces a missing numeric field to 0 instead of dropping the array", () => {
    const parsed = DashboardUsageDailyListSchema.parse([
      { date: "2026-05-19", model: "claude-opus-4-7", input_tokens: 100 },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.output_tokens).toBe(0);
    expect(parsed[0]?.cache_read_tokens).toBe(0);
    expect(parsed[0]?.cache_write_tokens).toBe(0);
  });

  it("coerces a missing date key to \"\" so the rest of the series survives", () => {
    const parsed = DashboardUsageDailyListSchema.parse([
      { model: "claude-opus-4-7", input_tokens: 5 },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.date).toBe("");
  });

  it("coerces a missing agent_id key to \"\" for the agent-runtime panel", () => {
    const parsed = DashboardAgentRunTimeListSchema.parse([
      { total_seconds: 42, task_count: 3, failed_count: 0 },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.agent_id).toBe("");
  });

  it("coerces a missing agent_id key to \"\" for the usage-by-agent panel", () => {
    const parsed = DashboardUsageByAgentListSchema.parse([
      { model: "claude-opus-4-7", input_tokens: 7 },
    ]);
    expect(parsed[0]?.agent_id).toBe("");
  });

  it("coerces missing fields on every runtime usage schema", () => {
    expect(RuntimeUsageListSchema.parse([{ date: "2026-05-19" }])[0]?.input_tokens).toBe(0);
    expect(RuntimeHourlyActivityListSchema.parse([{ hour: 9 }])[0]?.count).toBe(0);
    expect(RuntimeUsageByAgentListSchema.parse([{ model: "x" }])[0]?.agent_id).toBe("");
    expect(RuntimeUsageByHourListSchema.parse([{ hour: 9 }])[0]?.model).toBe("");
  });

  it("rejects a non-array body so parseWithFallback can return its fallback", () => {
    expect(DashboardUsageDailyListSchema.safeParse(null).success).toBe(false);
    expect(RuntimeUsageListSchema.safeParse({ rows: [] }).success).toBe(false);
  });

  it("keeps unknown server-side fields via .loose()", () => {
    const parsed = RuntimeUsageListSchema.parse([
      { date: "2026-05-19", region: "us-east" },
    ]);
    expect((parsed[0] as Record<string, unknown>).region).toBe("us-east");
  });
});

describe("ChatSessionSchema runtime_id + drift", () => {
  const base = {
    id: "11111111-1111-1111-1111-111111111111",
    workspace_id: "ws-1",
    agent_id: "agent-1",
    creator_id: "user-1",
    title: "Hello",
    status: "active",
    runtime_id: "rt-1",
    has_unread: false,
    created_at: "2026-06-08T00:00:00Z",
    updated_at: "2026-06-08T00:00:00Z",
  };

  it("preserves an explicit runtime_id binding", () => {
    expect(ChatSessionSchema.parse(base).runtime_id).toBe("rt-1");
  });

  it("defaults runtime_id to \"\" when an older server omits the field", () => {
    const { runtime_id, ...withoutRuntime } = base;
    void runtime_id;
    expect(ChatSessionSchema.parse(withoutRuntime).runtime_id).toBe("");
  });

  it("defaults has_unread to false on single-session fetches that omit it", () => {
    const { has_unread, ...withoutUnread } = base;
    void has_unread;
    expect(ChatSessionSchema.parse(withoutUnread).has_unread).toBe(false);
  });

  it("downgrades an unknown status to active instead of throwing (enum drift)", () => {
    expect(ChatSessionSchema.parse({ ...base, status: "frozen" }).status).toBe("active");
  });

  it("falls back to EMPTY_CHAT_SESSION when a required field is the wrong type", () => {
    const parsed = parseWithFallback(
      { ...base, id: 12345 },
      ChatSessionSchema,
      EMPTY_CHAT_SESSION,
      { endpoint: "test" },
    );
    expect(parsed).toEqual(EMPTY_CHAT_SESSION);
  });

  it("returns the fallback for a non-array list body", () => {
    const parsed = parseWithFallback(
      { sessions: [] },
      ChatSessionListSchema,
      [],
      { endpoint: "test" },
    );
    expect(parsed).toEqual([]);
  });
});

describe("GoalRunSchema + GoalSubtaskSchema drift", () => {
  const subtask = {
    id: "st-1",
    goal_run_id: "gr-1",
    seq: 1,
    title: "Backend API",
    spec: "build it",
    assignee_agent_id: "agent-1",
    depends_on: ["st-0"],
    status: "running",
    attempt: 1,
    max_attempts: 2,
    failure_reason: "",
  };
  const base = {
    id: "gr-1",
    workspace_id: "ws-1",
    squad_id: "sq-1",
    chat_session_id: "cs-1",
    title: "Refactor login",
    goal: "do the thing",
    status: "executing",
    subtasks: [subtask],
    created_at: "2026-06-09T00:00:00Z",
    updated_at: "2026-06-09T00:00:00Z",
  };

  it("parses a well-formed goal run with subtasks", () => {
    const parsed = GoalRunSchema.parse(base);
    expect(parsed.status).toBe("executing");
    expect(parsed.subtasks).toHaveLength(1);
    expect(parsed.subtasks[0]?.depends_on).toEqual(["st-0"]);
  });

  it("downgrades an unknown goal status to discussion (enum drift)", () => {
    expect(GoalRunSchema.parse({ ...base, status: "warp" }).status).toBe("discussion");
  });

  it("downgrades an unknown subtask status to pending (enum drift)", () => {
    const parsed = GoalSubtaskSchema.parse({ ...subtask, status: "teleporting" });
    expect(parsed.status).toBe("pending");
  });

  it("defaults kind to execute and verdict to '' when an older server omits them", () => {
    const { kind, verdict, ...without } = subtask as Record<string, unknown>;
    void kind;
    void verdict;
    const parsed = GoalSubtaskSchema.parse(without);
    expect(parsed.kind).toBe("execute");
    expect(parsed.verdict).toBe("");
  });

  it("downgrades an unknown kind to execute and unknown verdict to '' (enum drift)", () => {
    const parsed = GoalSubtaskSchema.parse({ ...subtask, kind: "supervise", verdict: "maybe" });
    expect(parsed.kind).toBe("execute");
    expect(parsed.verdict).toBe("");
  });

  it("preserves a verify node with a pass verdict", () => {
    const parsed = GoalSubtaskSchema.parse({ ...subtask, kind: "verify", verdict: "pass" });
    expect(parsed.kind).toBe("verify");
    expect(parsed.verdict).toBe("pass");
  });

  it("defaults depends_on to [] when an older server omits/nulls it", () => {
    const { depends_on, ...without } = subtask;
    void depends_on;
    expect(GoalSubtaskSchema.parse(without).depends_on).toEqual([]);
  });

  it("defaults handoff fields to empty strings when an older server omits them", () => {
    const parsed = GoalSubtaskSchema.parse(subtask);
    expect(parsed.upstream_output).toBe("");
    expect(parsed.handoff_brief).toBe("");
  });

  it("preserves handoff fields when the server exposes prompt context", () => {
    const parsed = GoalSubtaskSchema.parse({
      ...subtask,
      upstream_output: "### Analyze essence\nResult: useful context",
      handoff_brief: "Current target: Generate names",
    });
    expect(parsed.upstream_output).toContain("useful context");
    expect(parsed.handoff_brief).toContain("Generate names");
  });

  it("defaults subtasks to [] when the field is missing", () => {
    const { subtasks, ...without } = base;
    void subtasks;
    expect(GoalRunSchema.parse(without).subtasks).toEqual([]);
  });

  it("defaults persist fields when an older server omits them", () => {
    // base intentionally omits persist_task_id / can_persist (added later).
    const parsed = GoalRunSchema.parse(base);
    expect(parsed.persist_task_id).toBe("");
    expect(parsed.can_persist).toBe(false);
  });

  it("preserves persist fields when the server provides them", () => {
    const parsed = GoalRunSchema.parse({
      ...base,
      persist_task_id: "task-9",
      can_persist: true,
    });
    expect(parsed.persist_task_id).toBe("task-9");
    expect(parsed.can_persist).toBe(true);
  });

  it("defaults attribution fields when an older server omits them", () => {
    // base omits agent_name/runtime_name/model on subtask + coordinator_* on run.
    const parsed = GoalRunSchema.parse(base);
    expect(parsed.subtasks[0]?.agent_name).toBe("");
    expect(parsed.subtasks[0]?.runtime_name).toBe("");
    expect(parsed.subtasks[0]?.model).toBe("");
    expect(parsed.coordinator_name).toBe("");
    expect(parsed.coordinator_model).toBe("");
  });

  it("preserves attribution fields when the server provides them", () => {
    const parsed = GoalRunSchema.parse({
      ...base,
      subtasks: [{ ...subtask, agent_name: "coder", runtime_name: "Local", runtime_provider: "claude", model: "claude-opus-4-8" }],
      coordinator_name: "PMO",
      coordinator_runtime_name: "Cloud",
      coordinator_runtime_provider: "codex",
      coordinator_model: "gpt-5",
    });
    expect(parsed.subtasks[0]?.agent_name).toBe("coder");
    expect(parsed.subtasks[0]?.runtime_name).toBe("Local");
    expect(parsed.subtasks[0]?.model).toBe("claude-opus-4-8");
    expect(parsed.coordinator_name).toBe("PMO");
    expect(parsed.coordinator_model).toBe("gpt-5");
  });

  it("falls back to EMPTY_GOAL_RUN when a required field is the wrong type", () => {
    const parsed = parseWithFallback(
      { ...base, id: 999 },
      GoalRunSchema,
      EMPTY_GOAL_RUN,
      { endpoint: "test" },
    );
    expect(parsed).toEqual(EMPTY_GOAL_RUN);
  });

  it("falls back to EMPTY_GOAL_RUN on a null body", () => {
    const parsed = parseWithFallback(null, GoalRunSchema, EMPTY_GOAL_RUN, { endpoint: "test" });
    expect(parsed).toEqual(EMPTY_GOAL_RUN);
  });
});
