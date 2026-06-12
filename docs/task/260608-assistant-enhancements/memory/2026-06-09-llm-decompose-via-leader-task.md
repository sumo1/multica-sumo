# LLM 自动拆解 = 派规划任务给 squad leader（后端不直接调 LLM）

## 背景

需求二要「PMO 基于目标自动拆解工作流」。直觉上像是「后端调一次 LLM 拆解」，但 multica 架构不允许这么做。

## 结论（最关键的架构约束）

**multica 后端从不直接调 LLM。所有 AI 工作经 `daemon → agent → runtime`（本机真实 CLI 进程：codex / claude-code）。**

所以「LLM 拆解」= **派一个规划任务给 squad leader（PMO）**，和现有 squad 委派、quick-create 写回是同一模式：

```
POST /api/goals {auto_decompose:true}
  → StartPlanning：建 goal(status=planning) + 派规划任务给 leader
     （复用 quick-create 任务形态：CreateQuickCreateTask，context JSONB 带 goal 文本，无 FK）
  → daemon claim：注入 squad roster（成员名/角色/UUID，复用 buildSquadRoster）到 leader instructions
     + buildGoalPlanningPrompt（要求拆解+分配角色+声明依赖+用 CLI 写回，不要自己执行）
  → leader 产出 JSON 计划 → `multica goal plan <id> --subtasks-stdin`
  → POST /api/goals/{id}/plan → SubmitPlan：落子任务+DAG → executing → dispatch 根
  → 执行闭环接管（完成解锁下游 / 失败阻塞 / 上卷）
```

## 踩过的坑（同类 bug 出现两次）

`ResolveTaskWorkspaceID` 对「无 issue/chat/autopilot FK」的任务返回空 → `broadcastTaskEvent` 早退 → **task:* 事件从不发布 → 完成钩子永不触发**。goal subtask 任务、goal planning 任务都中过这个招（quick-create 当年也中过，代码注释写着）。**任何走 context-JSONB-无-FK 的新任务类型，都必须在 ResolveTaskWorkspaceID 加一个分支**，否则它的事件会被静默丢弃。

另一个坑：SubmitPlan/CreateGoal 返回的 `created` 是 dispatch 前快照（子任务还是 'ready'），dispatch 把根改成 'running' 后未回读 → 响应状态滞后。两路都要 dispatch 后 re-list。HTTP 集成测试第一次跑就抓到。

## 证据

来源：2026-06-09 实现期。架构约束是和用户多轮确认的结论（用户问「这个环境变量真的有必要吗，能直接复用本机进程吗」→ 引出「后端不调 LLM，复用 daemon→agent 链」）。坑是写 HTTP 集成测试时实测发现。相关 [[2026-06-09-goal-tables-not-autopilot]]。
