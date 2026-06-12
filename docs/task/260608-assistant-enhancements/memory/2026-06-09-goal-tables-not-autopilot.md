# goal 编排：新建 goal_run/goal_subtask，而非扩展 autopilot_run（决策翻转）

## 背景

design.md 2.3 原定「复用并扩展 autopilot_run」承载 goal 编排（省一张表）。进 L0 实施、摸了 autopilot_run 实表后，这个判断**翻转**了。

## 结论

新建独立的 `goal_run` + `goal_subtask` 两张表（migration 112），不扩展 autopilot_run。

翻转证据（autopilot_run 实际 schema，migration 042 + 096）：

- `autopilot_id UUID NOT NULL` —— 每行强制属于一个 autopilot。goal 没有 autopilot，复用要么改 nullable（破坏现有约束），要么给每个 goal 造假 autopilot（更脏）。
- `source TEXT NOT NULL CHECK (source IN ('schedule','manual','webhook','api'))` —— 无 'goal' 值，得改 CHECK。
- `task_id UUID`（1:1 单任务）—— goal 是 1:N，得拆。

三处约束都要动 = 违反 "Never break userspace"。新建表零破坏，只多一张表。

## 关键数据结构

- `goal_run`：编排容器，挂 `squad_id`（PMO=leader_id）+ `chat_session_id`。状态机 discussion→confirmed→planning→executing→completed/partial/failed/cancelled。
- `goal_subtask`：DAG 节点。`depends_on UUID[]`（DAG 边，空数组=根可立即派）、`assignee_agent_id`、`attempt/max_attempts`。状态 pending→ready→running→completed/failed/blocked/skipped。
- `agent_task_queue.goal_subtask_id`：执行回链，照抄 `autopilot_run_id` 的 nullable 外链模式，WS `task:*` 事件天然映射回树节点。
- **团队零新表**：squad 直接当编排层。

## 这意味着什么

显式列表路 `CreateGoal(confirmed=true)` 与自动拆解路 `SubmitPlan` 共用 `persistSubtasks` + `dispatchReadySubtasks`——拆解智能与执行管线正交。

## 证据

来源：2026-06-09 L0 实施期 Explore agent 摸 `server/migrations/042_autopilot.up.sql` + `096_autopilot_squad_assignee.up.sql` 实表列。属于「看了实表才知道扩展会破坏约束」的事实，无法从设计文档推出。
