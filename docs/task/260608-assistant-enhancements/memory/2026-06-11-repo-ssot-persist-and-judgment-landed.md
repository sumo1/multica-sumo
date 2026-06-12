---
name: repo-ssot-persist-and-judgment-landed
description: repo-SSOT 轮的后端落地——goal_persist 一键持久化 + goal_decision 下一步判断都已写码+实机机制验证通过；本轮范围与刻意未做项
metadata:
  type: project
---

`design-repo-ssot-task-env.md` 的施工图已落地（后端 + 前端 + 测试全绿）。沉淀本轮做了什么、刻意没做什么，避免下轮重复摸索。

## 已落地（②④ 为主，③ 部分）

- **② goal_persist（一键持久化到工程）**：新任务类型，完全照 `goal_summary` 模式（FK-less + context JSONB）。
  - service `PersistGoal`（`goal.go`）：gating 上游工程必须挂 `local_directory`；派 `goal_persist` 任务给总控 agent；**slug 用 `run.CreatedAt` 派生**（不是 now），保证重复点同一目录覆盖（快照语义）。`goalTaskSlug`/`kebabCase` 保留 CJK。
  - daemon claim（`daemon.go`）：persist 任务**额外 surface `ProjectID + ProjectResources`**——这是关键，daemon 靠 `findLocalDirectoryAssignment(ProjectResources)` 决定 agent 工作目录；不带它 agent 没 repo 可写。其它 goal 任务（planning/summary/decision）都不需要。
  - prompt `buildGoalPersistPrompt`：让 agent 在 `docs/task/{slug}/` author `progress.md` + `plan/step-*.md` 双契约；硬约束只写 docs/task、不改源码、不 commit、不写凭证。
  - HTTP `POST /api/goals/{id}/persist` → 202 `{persist_task_id}`；`GetGoal` 响应加 `can_persist`（gating）+ `persist_task_id`（已沉淀）。
  - 前端：tasks-page 常驻「持久化到工程」按钮（`FolderGit2`），`disabled=!can_persist`，i18n 4 语言。

- **④ goal_decision（下一步判断）**：执行引擎实质改动。
  - `handleSubtaskTerminal` 拆成 `unblockDownstream`/`blockDownstream`/`handleSubtaskFailure`。**失败边不再无脑 cascade-block**：有下游 + 有可用总控 → 派 `goal_decision` 任务，下游留 pending、goal 留 executing。
  - 总控经 `multica goal decide <subtask> proceed|reshape|abort` 回写（`DecideSubtask`）：proceed=skip 失败节点→解锁下游；reshape=改 spec+rearmAndDispatch；abort=block 下游（=旧行为）。复用现有 intervention transition，零新状态。
  - **fail-safe（关键）**：无总控 / 判断任务结束但没 decide（节点仍 failed）→ 退化成 block（旧行为）。最坏情况 = 今天的行为，绝不卡死。`SyncDecisionFromTask` 兜底。
  - 本轮**只在失败边**插判断；成功边"高风险 flag"留下轮（design §七已 defer 怎么标记）。

- 三处 FK-less workspace 解析（`ResolveTaskWorkspaceID` / `goalContextWorkspaceID`）都登记了新 type——**这个坑前两轮中招两次**，persist + decision 都已加，否则 task:message WS 广播被丢、④ 流空。

## 刻意没做（下轮）

- **worktree 并行**：harness `.claude/worktrees/` 约定成熟，但本轮先单 worktree 串行跑通。
- **③ 施工图双契约的 DB 承载**：目前 persist 时让 agent 从 digest 现场生成双契约；没有把双契约结构塞进 `goal_subtask.spec` 或新列。规划态产出结构化双契约留下轮。
- **执行历史视图**（读 repo `docs/task/*` 呈现历史任务）：未做。
- **改名**：保留"总控"（用户明确要求）；只清了 user-facing PMO 文案（i18n 4 语言 + `main_session`/`planning_hint`/`final_summary`/`summarizing`），**代码注释里的 PMO 没动**（~50 处，broad refactor 不划算）。

## 验证

- 全 Go 套件 + 全前端 typecheck/test/lint 绿。新增 7 个机制测试：persist 派发/快照可重复/gating、claim 带 repo 资源、判断失败路/成功路(proceed)/fail-safe/无总控退化。
- 沿用 [[daemon-agent-env-bedrock-403]] 的教训：验证到"机制跑通"（任务派发 + context 正确 + claim 带齐资源），不实机等 LLM 真写文件。
- 关联：[[design-repo-ssot-task-env 见 ../design-repo-ssot-task-env.md]]、[[llm-decompose-via-leader-task]]（后端不调 LLM）、[[pmo-summary-closeout]]（goal_summary 模式被 persist 复用）、[[execution-output-visibility]]（FK-less workspace 解析坑）。
