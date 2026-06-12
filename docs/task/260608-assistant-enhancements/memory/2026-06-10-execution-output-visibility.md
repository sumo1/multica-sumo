---
name: execution-output-visibility
description: How the Task page ④ column surfaces subtask process (task_messages) + result; the API enrichment that feeds it
metadata:
  type: project
---

The Task page ④ column shows the **live execution stream + final result** for the
PMO main session and each subtask — not just a "执行成功" status. This closes the
dogfood complaint "case 的执行结果和过程内容我看不到".

**Data path (verified end-to-end on the real desktop client 2026-06-10):**

- Backend `enrichGoalResponse(ctx, resp, run)` in `server/internal/handler/goal.go`
  fills `GoalRunResponse.planning_task_id` (via `GetPlanningTaskForGoal`) and each
  `GoalSubtaskResponse.task_id` (via `GetLatestTaskForSubtask`) + `result`. Called
  from `GetGoal`, `writeGoalForSubtask`, `ListTasks`, `writeTaskGoal`. **Easy to
  miss a call site** — if a new goal-returning handler skips it, the UI silently
  loses the stream affordance.
- Frontend `TaskStream({taskId, running, emptyHint})` in
  `packages/views/tasks/components/task-stream.tsx` is a **data-only wrapper**:
  fetch `taskMessagesOptions(taskId)` + `buildTimeline()`, hand to the shared
  `<TimelineView>`. Live updates ride the global `task:message` WS handler
  (use-realtime-sync) writing the `["task-messages", taskId]` cache.

**LIVE-STREAM DROP BUG (2026-06-10, third pass — "规划任务/已完成子任务都看不到输
出"):** messages were *persisted* (a later GET showed them) but the ④ column stayed
empty **during and right after execution**. Root cause: `ReportTaskMessages`
(`server/internal/handler/daemon.go`, the daemon endpoint that records each message
+ broadcasts `task:message` over WS) resolved the workspace with its OWN inline
issue/chat-only logic — for goal tasks `workspaceID` came back `""`, so the
`if workspaceID != ""` guard SKIPPED the broadcast. The front-end's
`taskMessagesOptions` uses `staleTime: Infinity` and fetches once when `task_id`
first appears (empty, task just dispatched), then relies entirely on the WS push to
fill — which never came. Fix: replace the inline resolution with the shared
`h.TaskService.ResolveTaskWorkspaceID(ctx, task)` (handles issue/chat/autopilot/
quick-create/**goal_subtask**/**goal_planning**). Same root-cause class as the
earlier `broadcastTaskEvent` drop. **Lesson: any handler that resolves a task's
workspace MUST use the shared resolver — never re-derive it from a subset of FK
columns.** Locked by `TestReportTaskMessagesBroadcastsForGoalTask` (subscribes to
the bus, asserts the broadcast fires) + `TestGoalPlanningWorkspaceID` (pure resolver
unit, the planning branch reads only context JSONB). Server rebuilt + restarted
(note: the live dev server runs off `/tmp/multica-server-new` with
`DATABASE_URL=…/multica`, NOT `.env`'s `local_medeo`; JWT_SECRET matches `.env` so
the desktop stays logged in).
- `SubtaskOutput` (in tasks-page.tsx) = spec header + `TaskStream(subtask.task_id)`.
  Falls back to `<Markdown>{result}</Markdown>` only when there's no stream. Main
  session = `TaskStream(goal.planning_task_id)` once planning runs, else discussion.

**Visualization refactor (2026-06-10, second pass — review feedback "看不清段落/要
markdown"):** the first cut rendered raw JSON `<pre>` blobs with no thinking-vs-
answer split. Fix = **reuse, not reinvent**. The chat list already had `TimelineView`
(preface text · collapsible process-fold · final markdown answer). Extracted it +
`splitTimeline` to shared `packages/views/common/task-transcript/` (`timeline-view.tsx`
+ build-timeline.ts), and both the chat list and the Task page now import the one
renderer. `chat/lib/copy-text.ts` re-exports `splitTimeline` from the shared module
(no duplicate). Everything text-like goes through `<Markdown>` → headings/lists/
tables/code render. Verified live on the desktop client.

**Full-height layout gotcha (2026-06-10, "拉到底也读不全"):** TasksPage and
AssistantPage rooted with `h-screen` (100vh). But these pages mount *below* the
app top bar + tab strip, so 100vh pushes the root's bottom — and each scroll
column's `overflow-y-auto` end — ~48–80px past the window edge. Symptom: you
scroll to the apparent bottom but the tail is off-screen. Fix: `h-full min-h-0`
(fill the bounded route container, the convention every detail page uses — issue-
detail, project-detail). Also add `min-h-0` to every flex column wrapper in the
chain (②③④), because a flex item defaults to `min-height:auto` and won't shrink
below its content, re-breaking the inner scroll bound. Geometry check via
computer-use: lowest AX element bottom == window bottom (0px overflow) confirms the
root is bounded. Live click re-verification was unreliable while a goal was
executing (3s poll + list reorder churns selection) — trust the geometry.

**Floating chat bubble removed:** `ChatFab` was the *only* opener of the floating
`ChatWindow` (store `toggle`/`setOpen(true)` called nowhere else). Removed BOTH from
the web `(dashboard)/layout.tsx` and desktop `desktop-layout.tsx` — removing just the
Fab would orphan the window. Assistant now lives in the sidebar 助理/任务 pages. The
`ChatFab`/`ChatWindow` components still exist (exported, unmounted); full deletion is
a separate cleanup if wanted.

Locked by `TestGoalResponseExposesResultAndTaskIDs` (cmd/server) — fails closed if
a handler drops result/task_id. See [[task-mode]], [[realmachine-verification-passed]].

**Test-setup lesson:** TasksPage renders ChatInput, which reads the singleton chat
store and throws "Chat store not initialised" if absent. tasks-page.test.tsx mocks
`@multica/core/chat` (useChatStore with `inputDrafts/setInputDraft/clearInputDraft`
+ getState, per the Object.assign pattern). Also added `Element.prototype.scrollTo`
polyfill to `packages/views/test/setup.ts` (jsdom gap; auto-scroll containers call
it). Both were async-after-assertion throws that passed tests but flagged as
unhandled errors — a false-positive risk.
