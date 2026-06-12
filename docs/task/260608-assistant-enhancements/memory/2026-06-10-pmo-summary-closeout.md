---
name: pmo-summary-closeout
description: PMO 收口/汇总步骤（goal_summary 任务）——主会话最终交付的来源；为什么、怎么连
metadata:
  type: project
---

PMO 规划任务是**一次性**的（规划 → 派发子任务 → 结束），所以它的 task_messages
stream 天然停在"节点 1 running"那一刻——主会话（④）没有最终结果。设计文档原意
PMO 职责含"汇总"，但初版只实现了规划+派发。本记忆是补上的**收口/汇总**步骤。

**机制（零 schema 改动）：**

- 新 context 类型 `GoalSummaryContextType = "goal_summary"`（镜像 `goal_planning`，
  FK-less，workspace/goal 信息全在 context JSONB）。结构体 `GoalSummaryContext`：
  GoalRunID/WorkspaceID/GoalTitle/Goal/Outcome/SubtaskDigest。
- `recomputeGoalStatus`（service/goal.go）：所有子任务终结时，**若有成功项**
  （completed>0）→ `maybeDispatchSummary` 派一个 summary 任务给 squad leader(PMO)，
  goal 留 `executing`；否则（全 failed）走 `finalizeGoalRun` 直接收口。
- `maybeDispatchSummary` 幂等：先 `GetSummaryTaskForGoal` 查重，已有就不再派。
  digest 由 `buildSubtaskDigest`（纯函数，读 []db.GoalSubtask 的 Result/FailureReason）
  拼成。没有可用 PMO（leader archived / 无 runtime）→ 返回 false，直接 finalize，
  **绝不把 goal 困在 executing**。
- 完成侧：`SyncSummaryFromTask`（goal listener 里和 `SyncPlanningFromTask` 并列调，
  各按自己 context type 过滤、互相 no-op）→ 读 context 的 Outcome → `finalizeGoalRun`。
- daemon：`buildGoalSummaryPrompt`（合成子任务产出成最终交付，**无 CLI 回写，回复即
  结果**）+ `BuildPrompt` 路由 `GoalSummaryRunID` + claim handler 注入 summary 字段。
- API：`GoalRunResponse.summary_task_id`（enrichGoalResponse 里 `GetSummaryTaskForGoal`）。
  前端 `GoalRun.summary_task_id`、schema、④ 主会话渲染 planning 流 + summary 流（带
  `task_page.final_summary` 分隔）。

**关键坑（又是工作区解析）：** summary 任务也 FK-less，必须让
`ResolveTaskWorkspaceID` 认它，否则它的 `task:message` WS 广播又被丢（同
[[execution-output-visibility]] 的根因）。已把 `goalPlanningWorkspaceID` 改名
`goalContextWorkspaceID`，同时认 `goal_planning` + `goal_summary`。**铁律：任何
FK-less goal 任务都要在这里登记，否则实时流静默失效。**

**测试：** `TestGoalDAGHappyPath`（cmd/server）改成驱动完整 summary 循环（完成子
任务 → 断言留 executing → drain summary 任务 → 完成 → 断言 completed）；
`TestGoalPlanningWorkspaceID` 加 summary-task 用例。实机：真 claude 跑通
plan→execute→verify→summary，PMO 产出最终交付，computer-use 截图确认 ④ 显示
planning 表格 + "最终交付（PMO 汇总）"段落。

关联：[[task-mode]]、[[execution-output-visibility]]、[[dynamic-workflows-verify-nodes]]、
[[llm-decompose-via-leader-task]]（PMO 汇总同样是"派任务给 PMO agent"，后端不调 LLM）。
