# 动态工作流：成员池 + 对抗验证节点（借鉴 Anthropic Dynamic Workflows）

## 背景

用户读了 Anthropic「Dynamic Workflows」文档（为每项任务即时编写专属 harness，6 种模式：分类分流/fan-out-合成/对抗验证/生成过滤/锦标赛/循环），要求：选团队 ≠ 锁定固定团队，而是定义成员池，PMO 基于目标动态设计工作流，按需引入对抗等角色。

## 结论

**执行模型差异（关键）**：Claude Code 的 dynamic workflow 是单进程 spawn 几十个轻量子 agent（共享上下文的分身）。multica 不同——每个节点是本机真实 CLI 进程（重、排队、隔离）。所以 multica 做**结构动态**（更聪明的真实任务 DAG），不是进程海量。第一刀挑「对抗验证」——文档里质量收益最大、最自然映射 DAG 的模式。

**实现（migration 113）**：
- `goal_subtask.kind`：`execute`（默认）/ `verify`；`goal_subtask.verdict`：`pass`/`reject`（仅 verify）。
- verify 节点 `depends_on` 被审 execute 节点，dispatch 时携带其产出（`buildReviewTarget`）。verify agent 用 `multica goal verdict <id> pass|reject --reason` 回写。
- **pass** → verify 完成 → 放行下游；**reject** → 被审节点 re-arm 重跑（attempt 受限）+ verify re-arm 重审，有界循环 A→V→reject→A→V→…→pass；attempt 耗尽仍 reject → 失败 → 阻塞下游。
- **fail-open**：verify agent 没回 verdict（忘了）→ 默认 pass + 告警。坏验证器不该卡死交付（务实，呼应「继续其余」）。

**成员池**：squad 从「固定执行团队」升级为「可调用角色库」。PMO 规划时从池里按目标动态选用（可只用一部分、可重复用、可插对抗节点）。团队 = 工作流节点候选池，非固定流程。零新表。

**PMO 规划升级为「工作流设计」**：buildGoalPlanningPrompt 教 PMO 基于目标+成员池决定结构（fan-out、在高风险产出后插 verify 节点用不同角色保证独立视角、trivial 步骤跳过验证省成本）。子任务 JSON 带 `kind`。

## 设计纪律（verdict 的单一权威）

verdict 经 CLI 回写时只持久化 verdict（SubmitVerdict 把节点 revert 回 running），**真正驱动工作流的是 task 完成钩子 handleVerifyCompleted**——「verify 完成」只有一条代码路径，避免双重处理。

## 后续可扩展

锦标赛/循环/分类分流等其余模式：加新 `kind` 即可，DAG 结构已留好位置。但每个 verify/对抗节点 = 双倍真实进程，文档自己警告「滥用 token 剧增」，要有并发/预算意识。

## 证据

来源：2026-06-09 用户提供 Anthropic Dynamic Workflows 文档（本地 HTML）+ 多轮对齐。模式取舍是和用户确认的（AskUserQuestion：squad 作成员池 + 第一刀做执行+对抗验证）。相关 [[2026-06-09-llm-decompose-via-leader-task]] [[2026-06-09-goal-tables-not-autopilot]]。
