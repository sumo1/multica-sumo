# 人工接管：失败子任务 → 绑该 agent 的 takeover 聊天会话

## 背景

design-goal-ui 五点五的失败处理最后一块：「人工接管 = 在③对话流直接跟该角色 agent 对话，手把手带它」。2026-06-09 落地。

## 方案

接管 = 在失败子任务上**新建一个 chat session**，绑定该子任务的执行 agent + runtime，标记 `goal_subtask_id`，让 agent 一进对话就带着子任务上下文（spec + 失败原因）。用户在熟悉的聊天界面手把手带它。

数据结构：`chat_session` 加 nullable `goal_subtask_id`（migration 114）。

- 为什么加列而非只靠 task context：接管对话要持续多轮，需要持久 session 实体；且 `goal_run.chat_session_id` 已有先例（规划讨论也绑 session），语义一致。
- daemon `buildChatPrompt` 在 session 有 `goal_subtask_id` 时，注入「## Takeover context」（subtask 标题 + 原 spec + 失败原因），agent 不冷启动。claim handler 在 chat 块里读 `cs.GoalSubtaskID` 填 TakeoverSubtask* 字段。

## 关键边界（避免耦合）

**接管只开对话，不动 goal/subtask 状态。** StartTakeover 创建 session 就返回，不改 subtask。用户在对话里带 agent 搞定后，再用现有「重试/跳过」推进 goal，或对话产出本身就是交付。这避免把 chat 流和 goal 状态机耦合死——单测专门验证「takeover 不改 subtask status」。

复用最大化：接管不发明新对话 UI。后端返回 ChatSession，前端 `onTakeover(sessionId)` 回调让助理页 `setActiveSession + setMode("chat")`——切到普通聊天界面，只是 agent 自带失败上下文。

## 落地物

| 层 | 产出 |
|----|------|
| L0 | migration 114（chat_session.goal_subtask_id）；query `CreateTakeoverChatSession` |
| L1 service | `GoalService.StartTakeover`（建绑 agent 的 takeover session）|
| L1 daemon | claim handler 读 cs.GoalSubtaskID → TakeoverSubtask* 字段；buildChatPrompt 注入 Takeover context |
| L1 handler | `POST /api/goals/subtasks/{id}/takeover` → 返回 ChatSession（不是 goal，因为下一步是去聊天）|
| L2 前端 | api.takeoverGoalSubtask + useGoalIntervention.takeover；树加「接管」按钮；GoalPanel.onTakeover → 助理页切 chat 模式+激活 session |
| 测试 | Go：takeover 建会话/绑 agent/不改状态；daemon prompt 注入 takeover context；组件：按钮触发 |

## 证据

来源：2026-06-09 实现 + 单测。`TestGoalTakeoverCreatesChatSession`、`TestBuildChatPromptTakeoverContext`。相关 [[2026-06-09-failure-intervention]]。这是失败处理四件套（重试/改派/编辑 spec/跳过）+ 接管的最后一块。
