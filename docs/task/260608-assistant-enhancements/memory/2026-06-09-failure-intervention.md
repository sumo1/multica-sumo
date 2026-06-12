# 失败干预：升级裁决按钮（重试/改派/编辑 spec/跳过）

## 背景

design-goal-ui 五点五定的失败处理：自动重试 1-2 轮 → 升级到用户裁决，失败节点亮出干预按钮。2026-06-09 落地了这一层（接管=对话流带 agent 暂未做）。

## 四个操作（全栈）

| 操作 | 语义 |
|------|------|
| 重试 | fresh rearm（重置 attempt=0，**人工触发不受自动重试预算限制**——那个预算已经耗尽才升级到用户）+ dispatch |
| 改派 | 换 agent（校验同 workspace + 可运行）+ fresh rearm + dispatch |
| 编辑 spec | 改 spec（fix-then-retry）+ fresh rearm + dispatch |
| 跳过 | 标 skipped + 解锁下游（skipped 当非阻塞终结处理）|

共性：都先 `loadSubtaskForIntervention`（workspace 门禁）→ 操作 → `reviveGoalIfTerminal`（goal 从 partial/failed 回 executing）。前三个走 `rearmAndDispatch`，跳过走 `handleSubtaskTerminal(success=true)`。

## 关键修复（这次新发现）

`handleSubtaskTerminal` 的下游解锁原本只把 **'pending'** 依赖标 ready。但被失败上游卡住的节点是 **'blocked'**——skip/retry 一个失败节点后，它的 blocked 下游不会复活。改为接受 `pending || blocked`。

同时 dep-满足判断从「全 completed」放宽到「全 completed **或 skipped**」——用户跳过一个节点 = 主动决定越过它，下游应能跑。

## 设计决策

- 人工重试**重置 attempt 预算**（`RearmGoalSubtaskFresh` vs 自动路径的 `RearmGoalSubtask`）。人工介入是 override，不该被自动预算挡住。
- 干预端点返回**整个更新后的 goal**（不只是子任务），前端一次 round-trip 刷新整棵树。
- 前端 `useGoalIntervention` 返回 4 个 mutation；树的 `intervene` prop 可选，给了才在 failed/blocked 节点显示按钮（presentational 组件不直接调 API，保持可测）。

## 未做

接管（人工在对话流直接跟该角色 agent 手把手对话）——要接 chat 流，是另一个集成面。design-goal-ui 五点五的「人工接管形态」。

## 证据

来源：2026-06-09 实现 + 单测。query：`RearmGoalSubtaskFresh`/`SkipGoalSubtask`。端点 `POST /api/goals/subtasks/{id}/retry|reassign|edit-spec|skip`。相关 [[2026-06-09-dynamic-workflows-verify-nodes]]。
