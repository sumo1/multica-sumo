# 技术方案设计

> 任务 ID: 260608-assistant-enhancements ｜ 2026-06-08
> 基于四路 design 调研（证据见 `memory/2026-06-08-design-research-findings.md`）。
> 深度：需求一 → 可执行级（含双契约）；需求二/三/端形态 → 方案级。

## 总览：四块改动 + 一条主线

```
需求一  接通 runtime 选择       [可执行]  改动小，是整条链的练手
需求二  PMO 多角色并行编排       [方案]    编排层新建，站在 autopilot_run 肩上
需求三  computer-use 本机操作    [方案]    daemon 统一封装，CLI 子进程
端形态  desktop 拉本地 server    [方案]    路径 C
```

主线判断：**需求一是需求二的真实前置**——两者共享「会话 + runtime 选择 + daemon 执行链」。需求一打通后，goal 模式只是在同一对话上加一个模式开关。所以执行顺序 = 需求一先行。

---

## 一、需求一：接通 chat session 的 runtime（可执行级）

### 1.1 核心判断

**真问题不是「传个参数」，而是「任务分配读错了源」。**

调研证据（`task.go:626-655`）：发消息时 `EnqueueChatTask` 读的是 `agent.runtime_id`（agent 默认 runtime），**不是 `chat_session.runtime_id`**。所以即便把会话选的 runtime 存进 `chat_session.runtime_id`，任务仍会跑在 agent 默认 runtime 上。

数据库层早已就绪：`chat_session.runtime_id` 字段存在（migration 060，nullable）。缺的是两件事：①创建时接受用户选择并落库；②任务分配时优先读会话的 runtime。

### 1.2 数据流（改动前 → 改动后）

```
改动前：
  CreateChatSession(agent_id) → chat_session.runtime_id = agent.runtime_id（自动继承）
  SendChatMessage → EnqueueChatTask 读 agent.runtime_id → 任务跑在 agent 默认 runtime

改动后：
  CreateChatSession(agent_id, runtime_id) → chat_session.runtime_id = 用户选择（校验在线 + 归属）
  SendChatMessage → EnqueueChatTask 读 chat_session.runtime_id（回退 agent.runtime_id）→ 任务跑在会话选定 runtime
```

回退逻辑保证兼容：历史会话 `runtime_id` 为空时，仍走 agent 默认，行为不变。

### 1.3 改动点清单（path:line）

**后端：**

| 文件 | 改动 |
|------|------|
| `server/internal/handler/chat.go:26-29` | `CreateChatSessionRequest` 增 `RuntimeID *string`（可选，nil 时回退 agent 默认） |
| `server/internal/handler/chat.go`（CreateChatSession handler） | 若传 runtime_id：用 `parseUUIDOrBadRequest` 校验格式 + 查 runtime 属同 workspace。**online 校验本次不做**（仅前端拦截，见 1.4）。 |
| `server/pkg/db/queries/chat.sql:2-4` | INSERT 改为接受 `runtime_id` 参数：传入则用之，为 NULL 则 `SELECT runtime_id FROM agent` 回退 |
| `server/internal/service/task.go:626-655` | `EnqueueChatTask` 改读取优先级：`COALESCE(chat_session.runtime_id, agent.runtime_id)` |
| sqlc 生成 | `make sqlc` 重新生成 |

**前端：**

| 文件 | 改动 |
|------|------|
| `packages/core/types/chat.ts:1-12` | `ChatSession` 增 `runtime_id: string \| null` |
| `packages/core/api/client.ts:1570-1575` | `createChatSession` 签名增 `runtime_id?: string`；改用 `parseWithFallback` + 新 schema |
| `packages/core/api/schemas.ts`（或 schema.ts） | 新增 `ChatSessionSchema` + `EMPTY_CHAT_SESSION`（补 CLAUDE.md 要求的边界防御） |
| `packages/core/chat/mutations.ts:15` | `useCreateChatSession` mutationFn 透传 `runtime_id` |
| `packages/views/assistant/components/assistant-page.tsx:128-131` | `createSession.mutateAsync` 真正传 `runtime_id`（当前漏传的根因点） |
| `packages/views/agents/components/runtime-picker.tsx:159-162` | 离线 runtime 禁用：`useRuntimeHealth` 判断，`offline`/`recently_lost` 置 disabled + 提示 |
| `packages/views/assistant/components/new-session-dialog.tsx` | 会话头部/创建后展示绑定 runtime；`canCreate` 排除离线选择 |

### 1.4 离线阻止创建（仅前端，已定）

- **前端**：RuntimePicker 离线项禁用、不可选（`runtime-picker.tsx`，用 `useRuntimeHealth`）；NewSessionDialog `canCreate` 排除离线选择。
- **后端**：本次**不加**校验（用户决定）。

> 权衡留痕：仅前端拦截意味着「绕过前端直接调 API 传离线 runtime_id」仍能建会话，造出绑定不可变的死会话。影响面仅限非正常调用路径，最坏后果是一个空会话（非数据损坏），MVP 阶段可接受。**后端 online 校验列为后续加固项**，需要时在 CreateChatSession handler 加约 10 行。

「绑定不可变」自然成立：会话创建后不提供切换 runtime 入口，`chat_session.runtime_id` 创建后不再 UPDATE。

### 1.5 需求一双契约

#### 施工契约

- **可改文件**：上表 path 列出的文件。
- **不可改边界**：`agent_task_queue` 表结构、daemon claim 逻辑、WS 事件——均不动（任务分配按 runtime_id 的机制已工作）。
- **产出**：
  - 后端 `CreateChatSessionRequest.RuntimeID`、handler 校验、`chat.sql` 接受参数、`EnqueueChatTask` 读取优先级。
  - 前端类型 + API + dialog 传参 + RuntimePicker 离线禁用 + ChatSessionSchema。
- **约束**：runtime_id 为空必须回退 agent 默认（兼容历史会话）；schema 走 parseWithFallback。

#### 验收契约

| 验收项 | 命令 / 方法 | 通过标准 |
|--------|------------|---------|
| 类型检查 | `pnpm typecheck` | 0 error |
| Go 测试 | `cd server && go test ./internal/handler/ ./internal/service/` | 通过 |
| runtime_id 落库 | 创建会话传 runtime_id → 查 DB | `chat_session.runtime_id` = 所选值 |
| 任务分配生效 | 该会话发消息 → 查 `agent_task_queue.runtime_id` | = 会话 runtime（非 agent 默认） |
| 回退兼容 | runtime_id 为 NULL 的会话发消息 | 任务走 agent.runtime_id，行为不变 |
| 离线阻止（前端） | RuntimePicker 渲染离线 runtime | 该项 disabled，有提示；canCreate 排除离线 |
| ~~离线阻止（后端）~~ | — | 本次不做（仅前端拦截，见 1.4 权衡） |
| schema 防御 | 喂 malformed chat session 响应 | 走 fallback 不白屏（新增测试） |
| 绑定不可变 | 已建会话 | 无切换 runtime 入口；runtime_id 不被 UPDATE |

---

## 二、需求二：PMO 多角色并行编排（方案级）

### 2.1 核心判断

**底座够强，编排层要新建，但站在 `autopilot_run` 肩上而非从零。**

调研三个关键事实：
1. **并行 claim 零改造**——多 runtime 并行 claim 已工作，`max_concurrent_tasks` 控并发。
2. **拆解/编排必须在 server 侧**——daemon 只是单 task executor，无指令解析、无多步拆分。
3. **`autopilot_run`（migration 042）是最接近的现有机制**——有状态机、并发策略、squad 分配，但 `task_id` 是 1:1，需扩成「一个 run → 多子任务」。

### 2.2 PMO 是什么——一个编排实体，不是一个特殊 agent

PMO 不该做成「又一个 agent」，而是 **server 侧的编排实体（goal run）**，复用并扩展 `autopilot_run` 的形态：

```
goal run（编排容器，扩展自 autopilot_run 的理念）
  ├─ 规划阶段：派一个「规划角色」agent 生成子任务 DAG
  ├─ 子任务集：N 个 agent_task_queue 行，共享 goal_run_id
  │            依赖关系存在编排层（见 2.3）
  └─ 状态机：规划中 → 执行中（并行/串行）→ 完成/失败
```

「规划角色」「执行角色」都是普通 Agent（呼应已定的「复用现有 Agent」）。PMO 本身是状态机 + 调度逻辑，不是对话体。

### 2.3 PMO 实体：新建 goal_run / goal_subtask（已定，2026-06-09 翻转）

**决策：新建独立的 `goal_run` + `goal_subtask` 两张表（migration 112），不扩展 autopilot_run。**

> 这是对原「扩展 autopilot_run」判断的翻转。进 L0 实施时摸了实表（见 `memory/` 与下方证据），扩展方案会破坏现有 autopilot 语义，得不偿失。

翻转证据（autopilot_run 实际 schema，migration 042 + 096）：
- `autopilot_id UUID NOT NULL` —— 每行强制属于一个 autopilot。goal 没有 autopilot，要么把它改 nullable（破坏现有约束），要么给每个 goal 造假 autopilot（更脏）。
- `source TEXT NOT NULL CHECK (source IN ('schedule','manual','webhook','api'))` —— 无 'goal' 值，得改 CHECK。
- `task_id UUID`（1:1 单任务）—— goal 是 1:N，得拆。

复用三处约束都要动 = 违反 "Never break userspace"。新建表零破坏、语义干净，只多一张表。

**实际落地的表结构（migration 112，已 apply 到 multica 库并验证）：**
- `goal_run`：编排容器。挂 `squad_id`（团队，PMO=leader）+ `chat_session_id`（驱动对话）。状态机 `discussion → confirmed → planning → executing → completed/partial/failed/cancelled`，对齐五点五的三阶段 + 失败「部分完成」。
- `goal_subtask`：DAG 节点。`depends_on UUID[]`（指向兄弟节点，空数组=根可立即派）、`assignee_agent_id`（执行角色）、`spec`（可在失败升级时编辑）、`attempt/max_attempts`（自动重试 1-2 轮）、状态 `pending → ready → running → completed/failed/blocked/skipped`。
- `agent_task_queue.goal_subtask_id`：执行回链，照抄 `autopilot_run_id` 的 nullable 外链模式，WS `task:*` 事件天然映射回树节点。

squad 完全够当「团队」层（leader_id=PMO、squad_member=角色、instructions、ListSquadMemberStatus），零新表。

sqlc 查询见 `server/pkg/db/queries/goal.sql`，已 `sqlc generate` 通过、生成代码编译通过。

**并行判定**（呼应 README 三条）由规划角色在拆解时给出 + PMO 校验：文件范围互斥、约束显式、验收独立——三条满足才标同一并行组。

### 2.3.1 失败处理（已定）：继续其余 + 标记失败

- 并行组内某子任务失败：**不阻断同组其余子任务**，该子任务状态标记 failed（右侧状态栏标红）。
- 仅**依赖它的下游**子任务被阻塞。
- PMO 汇总后交用户决定重试 / 放弃（不自动重试整组）。
- 这与「状态可观测」一致，也保住并行优势。fail-fast（整组停）作为可选策略后续再加。

### 2.4 goal 模式触发与执行流

```
用户在对话框选 goal 模式 + 输入复杂目标
  → server 创建 goal_run，派规划角色 agent（带 /goal 或自然语言前缀，见 2.5）
  → 规划角色产出子任务 DAG → 落 goal_run.plan
  → PMO 调度：无依赖的子任务入队（agent_task_queue，各带 runtime_id）
     并行组同时 queued → 多 runtime 并行 claim（已支持）
  → 子任务完成 → PMO 检查依赖 → 解锁下游子任务入队
  → 全部完成 → goal_run 完成
```

### 2.5 daemon 侧改动（小）

- `BuildPrompt`（`prompt.go:17`）：goal 类任务的 prompt 前缀注入 `/goal`（codex 大写 `/Goal`）或自然语言「通过目标模式，完成 …」。
- 传 goal 上下文：类比现有 `task.AutopilotRunID`，加 `task.GoalRunID` 透传。
- 进度回流：**无需改**——现有 `task:message`（500ms flush）+ `task:*` 事件族已够细。

### 2.6 状态视图（右侧栏）数据源与复用

- **数据源现成**：`task:queued/dispatch/running/progress/completed/failed` WS 事件，`use-realtime-sync.ts` 已订阅。
- **可复用 UI**：`ExecutionLogSection`（issue 任务面板，active/past 分桶）+ `TaskTranscript`（消息时间线）是现成原型。
- **需新建**：goal_run 维度的聚合（把 N 个子任务按 DAG 组织展示）、依赖/进度可视化。三区对应：任务/改动区（goal 概览）、进度区（子任务状态列表）、来源区（产物/trace）。

### 2.7 需求二收尾确认（已定）

1. ~~goal_run 新建 vs 扩展~~ → **已定（翻转）：新建 goal_run + goal_subtask 独立表**（见 2.3，证据为 autopilot_run 的 NOT NULL/CHECK/1:1 约束不可破）。
2. ~~DAG 存 JSONB vs 关系表~~ → **已定（翻转）：用 goal_subtask 行 + depends_on UUID[] 显式表达 DAG**，比 JSONB 更可查询、可单节点更新（重试/改派/编辑 spec）。
3. ~~失败处理~~ → **已定：自动重试 1-2 轮 → 升级裁决；继续其余 + 阻塞下游**（见 design-goal-ui.md 五点五）。
4. L0 已落地：migration 112 + goal.sql + sqlc 生成，均验证通过。

### 2.8 薄垂直切片实现状态（已落地，2026-06-09）

按「先打通最薄端到端链」原则实现，证明 `goal→子任务→角色→派发→完成解锁→树更新` 整条闭环成立。

**已实现（全部验证通过）：**

| 层 | 产出 | 验证 |
|----|------|------|
| L0 数据 | migration 112（goal_run/goal_subtask/agent_task_queue.goal_subtask_id）+ goal.sql + sqlc | apply 到 multica 库，三表核对 |
| L1 service | `server/internal/service/goal.go`：CreateGoal（拆解+DAG 两遍 wiring）、ConfirmGoal（确认闸门）、dispatchReadySubtasks、SyncSubtaskFromTask（完成钩子）、handleSubtaskTerminal（解锁/阻塞级联）、recomputeGoalStatus（completed/partial/failed 上卷）、自动重试 attempt<max | 2 个 DB 集成测试（happy path + 失败阻塞下游）|
| L1 执行路 | 复用 QuickCreate 形态：goal_subtask context JSONB + goal_subtask_id 回链；daemon `buildGoalSubtaskPrompt`；claim handler 注入 spec | prompt 单测 |
| L1 事件 | `goal:run_updated`/`goal:subtask_updated` 事件；`goal_listeners.go` 挂 EventTaskCompleted/Failed/Cancelled；**修复 ResolveTaskWorkspaceID 对 goal 任务返回空导致事件不发布**（与 quick-create 同类 bug）| 集成测试覆盖 |
| L1 handler | `server/internal/handler/goal.go`：POST /api/goals、GET /api/goals/{id}、POST /api/goals/{id}/confirm；UUID 解析合规 | build+vet |
| L2 前端 | types/goal.ts、GoalRunSchema/GoalSubtaskSchema（enum drift .catch + EMPTY_GOAL_RUN）、client 三方法、goals/ queries+mutations、WS goal: 失效 | 7 schema 测试（含 malformed/null fallback）|
| L2 UI | `GoalStatusTree`（Codex 进度条 + PMO 角色树，状态图标对齐 Claude Code agent-view）；chat 命名空间加 goal i18n（4 语言）| 5 组件测试（含 enum drift 不崩）|

**切片范围内刻意未做（后续叠加层）：**
- ~~LLM 自动拆解~~ → **已实现（见 2.9）**。
- 多轮讨论阶段 UI（discussion 状态机已就位，前端讨论交互未做）。
- 需求文档写入目标 repo（design-goal-ui 五点五）。
- 失败升级的干预按钮（重试/改派/编辑 spec/接管），handler 层 query 已备（UpdateGoalSubtaskSpec/ReassignGoalSubtask）。
- 助理页 4 栏整合（GoalStatusTree 已独立可挂载，待团队选择 UI）。

### 2.9 LLM 自动拆解（已落地，2026-06-09）

**核心认知**：multica 后端不直接调 LLM，所有 AI 工作经 `daemon → agent → runtime`。所以"LLM 拆解" = **派一个规划任务给 squad leader（PMO）**——和 squad 委派、quick-create 写回同一模式，不是后端调模型。

**流程**：
```
POST /api/goals {auto_decompose:true}
  → StartPlanning：建 goal(status=planning) + 派规划任务给 leader
     （复用 quick-create 任务形态：context JSONB 带 goal 文本，无 FK）
  → daemon claim：注入 squad roster（成员名/角色/UUID）到 leader instructions
     + buildGoalPlanningPrompt（要求拆解+分配角色+声明依赖+用 CLI 写回）
  → leader 产出 JSON 计划 → `multica goal plan <id> --subtasks-stdin`
  → POST /api/goals/{id}/plan → SubmitPlan：落子任务+DAG → executing → dispatch 根
  → (2.8 的执行闭环接管：完成解锁下游、失败阻塞、上卷)
```

**关键设计**：SubmitPlan 与 CreateGoal(confirmed=true) 共用 `persistSubtasks` + `dispatchReadySubtasks`——拆解智能与执行管线正交，规划 agent 只是产出喂给同一管线的子任务列表。显式列表路与自动拆解路最终汇流到同一执行引擎。

**落地物：**
| 层 | 产出 |
|----|------|
| service | StartPlanning、dispatchPlanningTask、SubmitPlan；GoalPlanningContext |
| 执行路 | 复用 CreateQuickCreateTask（context 带 goal_planning 类型）；ResolveTaskWorkspaceID 加 goal_planning 分支（否则规划任务事件不发布）|
| daemon | buildGoalPlanningPrompt（CLI 写回契约 + 不要自己执行）；claim 注入 squad roster（复用 buildSquadRoster）|
| handler | CreateGoal 加 auto_decompose 分支（与 subtasks 互斥）；SubmitPlan handler + POST /api/goals/{id}/plan 路由 |
| CLI | `multica goal plan <id> --subtasks/--subtasks-stdin`（cmd_goal.go）|
| 前端 | client.createGoal + CreateGoalInput 加 auto_decompose |
| 测试 | StartPlanning 派发、SubmitPlan DAG、**HTTP 端到端**（auto_decompose→plan→executing 全链，含互斥校验）、规划 prompt 单测 |

**修了一个 staleness bug**：SubmitPlan/CreateGoal 返回的 `created` 是 dispatch 前快照（子任务还是 'ready'），dispatch 把根任务改成 'running' 后未回读 → 响应状态滞后。两路都加了 dispatch 后 re-list。HTTP 集成测试第一次跑就抓到了它。

### 2.10 用户可见入口：GoalPanel + 助理页模式开关（已落地，2026-06-09）

把后端引擎接到 UI，让用户真能用——前面所有后端能力此前对真实用户不可见（GoalStatusTree 未挂载、无发起入口）。

**落地物：**
| 产出 | 说明 |
|------|------|
| `GoalPanel`（packages/views/assistant/components/goal-panel.tsx）| 左：团队(squad)选择 + 目标文本框 + 发起按钮（auto_decompose）；右：实时 GoalStatusTree。`goalRunOptions` 喂数据，planning/executing 时 3s 轮询（规划子任务异步落地），goal:* WS 事件失效缓存。agent 名称解析喂给树。|
| 助理页模式开关 | assistant-page.tsx 顶部「普通聊天 / 目标模式」切换；goal 模式渲染 GoalPanel，聊天逻辑零改动。|
| i18n | chat 命名空间加 panel/session_list/new_dialog（4 语言）。**顺带清掉 session-list/new-session-dialog 的历史硬编码中文 lint 债**，assistant 目录现全绿。|
| 测试 | GoalPanel 3 测试（empty 态 / 选团队→输目标→auto_decompose 发起→实时树渲染+角色名解析 / 无团队提示）。|

**验证**：views 1081/1081 + core 457/457 测试通过，6 包 typecheck 通过，assistant 目录 lint 全绿，locale parity 157 通过，零回归。

至此 goal 模式**端到端可用**：用户在助理页切到目标模式 → 选团队 → 描述目标 → PMO 自动拆解 → 实时看到子任务派发与状态流转。

### 2.11 动态工作流：成员池 + 对抗验证节点（已落地，2026-06-09）

借鉴 Anthropic「Dynamic Workflows」（为每项任务即时编写专属 harness，6 种模式：分类分流/fan-out-合成/对抗验证/生成过滤/锦标赛/循环）。

**执行模型差异（关键约束）**：Claude Code 的 dynamic workflow 是单进程内 spawn 几十个轻量子 agent。multica 不同——每个节点是 `goal_subtask → 队列 → daemon claim → 本机真实 CLI 进程`，重、排队、有隔离。所以 multica 做的是**结构动态**（更聪明的真实任务 DAG），而非进程海量。第一刀实现「对抗验证」——文档里质量收益最大、最自然映射 DAG 的模式。

**成员池（squad 复用，零新表）**：squad 从「固定执行团队」语义升级为「可调用的角色库」。PMO 规划时从成员池按目标动态选用——可只用一部分、可重复用、可按角色插对抗节点。团队成员不再是固定流程，而是工作流节点的候选池。

**节点类型化（migration 113）**：
- `goal_subtask.kind`：`execute`（做事，默认）/ `verify`（对抗审查）。
- `goal_subtask.verdict`：verify 节点的 `pass`/`reject`。
- verify 节点 `depends_on` 被审的 execute 节点，dispatch 时携带其产出（`buildReviewTarget`）；verify agent 用 `multica goal verdict <id> pass|reject --reason` 回写。
- **pass** → verify 节点 completed → 放行下游；**reject** → 把被审节点 re-arm 重跑（attempt 受限）+ verify 节点 re-arm 重审，形成有界循环 A→V→reject→A→V→…→pass；被审节点 attempt 耗尽仍被 reject → 失败 → 阻塞下游。
- **fail-open**：verify agent 没回 verdict（忘了）→ 默认 pass + 告警。坏验证器不该卡死交付（务实，呼应「继续其余」）。

**PMO 规划升级为「工作流设计」**：`buildGoalPlanningPrompt` 不再只是「拆子任务」，而是教 PMO 基于目标 + 成员池设计工作流结构——决定 fan-out、决定在高风险产出后插对抗验证节点（用不同角色保证独立视角）、trivial 步骤跳过验证省成本。子任务 JSON 带 `kind`。

**落地物：**
| 层 | 产出 |
|----|------|
| L0 | migration 113（kind + verdict CHECK 约束）；goal.sql 加 SetGoalSubtaskVerdict/RearmGoalSubtask；sqlc |
| L1 service | GoalSubtaskContext 加 kind/ReviewTarget；dispatchSubtask 为 verify 节点组装 review target；handleVerifyCompleted（pass 放行 / reject 有界重跑 / fail-open）；SubmitVerdict |
| L1 daemon | buildGoalVerifyPrompt（对抗审查 + verdict CLI 契约）；规划 prompt 升级为工作流设计 |
| L1 handler/CLI | CreateSubtaskInput 加 kind；SubmitVerdict handler + POST /api/goals/subtasks/{id}/verdict；`multica goal verdict` 命令 |
| L2 前端 | GoalSubtask 加 kind/verdict（enum drift .catch）；GoalStatusTree 渲染 verify 节点（盾牌图标 + 边框 + pass/reject 徽章）；i18n 4 语言 |
| 测试 | verify pass 放行下游 / verify reject 重跑被审节点（attempt bump）；verify prompt 单测；3 个 schema drift 测试；2 个组件渲染测试 |

**验证**：7 个 goal Go 测试全过（含两个 verify-node DAG 测试）+ daemon prompt 测试 + views 1083/1083 + core 460/460 + 6 包 typecheck + lint 全绿，零回归。

至此实现你要的：**选团队 = 选成员池而非固定流程；PMO 基于目标 + 可用成员动态设计工作流；按需引入对抗验证角色**。后续 kind 可继续扩展锦标赛/循环/分类分流——DAG 结构已为它们留好位置。

### 2.12 实机验证通过（真 LLM，2026-06-09）

在真 daemon + 真 claude-code 上端到端跑通（之前只有单测）。发 goal「为 AI 编排 CLI 起名」→ PMO **自主设计了带对抗验证的工作流**：① execute 头脑风暴（E2E Agent）→ ② verify 对抗评审（Hello Bot, role=reviewer, depends_on ①）。全链真实点火：派规划任务→真 claude 拆解→`multica goal plan` 写回→SubmitPlan 落 DAG→executing→① 跑完→解锁 ②→真 claude 对抗评审→`multica goal verdict pass`→handleVerifyCompleted finalize→goal completed。reviewer 真去读了产出、独立核验命名冲突，非橡皮图章。详见 `memory/2026-06-09-realmachine-verification-passed.md`。

**实机暴露并修复的两个 bug**（单测覆盖）：
- planning 任务失败后 goal 永卡 `planning`：listener 只看 goal_subtask_id，planning 任务无此字段被忽略 → 新增 `SyncPlanningFromTask`（planning 任务终结且 goal 仍 planning → 标 failed）。测试 `TestGoalPlanningFailureFailsGoal`。
- （此前已修）无 FK 任务的 task:* 事件被 `ResolveTaskWorkspaceID` 静默丢弃。

**环境注意**：daemon 二进制须与 server 一起重建（否则用旧 prompt 静默退化）；daemon 应以干净 env 启动靠 `resolveAgentEnvViaLoginShell` 拉 AI 凭据，手动部分注入会触发「已存在则跳过」导致配置撕裂（403）。见 `memory/2026-06-09-realmachine-daemon-must-rebuild.md`。

### 2.13 失败干预（升级裁决按钮，已落地，2026-06-09）

落地 design-goal-ui 五点五的「升级到用户裁决」——失败/阻塞节点上的干预手段。

**四个干预操作**（service + handler + 路由 + 前端 mutation + 树按钮全栈）：
- **重试**：fresh rearm（重置 attempt 预算——人工触发不受自动重试预算限制）+ dispatch；goal 从 partial/failed 复活回 executing。
- **改派**：换 agent（校验同 workspace）+ fresh rearm + dispatch。前端弹 agent 选择器。
- **编辑 spec**：改 spec（fix-then-retry）+ fresh rearm + dispatch。前端内联编辑框。
- **跳过**：标 skipped + 解锁下游（skipped 当非阻塞终结）。

**关键修复**：`handleSubtaskTerminal` 的下游解锁原本只 ready 'pending' 依赖；改为也接受 'blocked'（被失败上游卡住的节点）——否则 skip/retry 后下游不会复活。dep-满足判断也从「全 completed」放宽到「全 completed 或 skipped」。

接管（在对话流跟 agent 对话）**未做**——它要接 chat 流，是另一个集成面，留下一块。

**端点**：`POST /api/goals/subtasks/{id}/retry|reassign|edit-spec|skip`，均返回整个更新后的 goal（树一次刷新）。query：`RearmGoalSubtaskFresh`（重置 attempt）、`SkipGoalSubtask`。

**验证**：4 个干预 Go 测试（retry 复活 goal / skip 解锁下游 / reassign 换 agent / edit-spec 改 spec）+ 4 个组件测试（按钮触发、running 节点不显示、编辑框提交、busy 禁用）+ 全 12 goal Go 测试 + views 1087/1087 + core 460/460 + 6 包 typecheck + lint 全绿，零回归。

### 2.14 人工接管（失败处理最后一块，已落地，2026-06-09）

接管 = 失败子任务上**新建一个 chat session**，绑该子任务的执行 agent + runtime，标记 `goal_subtask_id`（migration 114），daemon `buildChatPrompt` 注入「Takeover context」（subtask 标题/原 spec/失败原因）让 agent 不冷启动。用户在熟悉的聊天界面手把手带它。

**关键边界**：接管只开对话，**不动 goal/subtask 状态**——避免 chat 流和 goal 状态机耦合死。用户带 agent 搞定后再用重试/跳过推进。复用最大化：后端返回 ChatSession，前端 `onTakeover` 让助理页切 chat 模式+激活该 session，看到的就是普通对话。

落地：query `CreateTakeoverChatSession`、`GoalService.StartTakeover`、`POST /api/goals/subtasks/{id}/takeover`（返回 ChatSession）、前端 takeover mutation + 树「接管」按钮 + 助理页模式切换。验证：takeover 建会话/绑 agent/不改状态 Go 测试 + daemon prompt 注入测试 + 组件测试 + views 1088/1088 + core 460/460，零回归。详见 `memory/2026-06-09-human-takeover.md`。

至此 design-goal-ui 五点五的失败处理**全部落地**：自动重试 → 升级裁决（重试/改派/编辑 spec/跳过）→ 人工接管。

---

## 三、需求三：集成 computer-use 作为 CLI + Skill（方案级，已重写）

### 3.0 认知演进（2026-06-08，三次纠正）

| 方案 | 状态 | 原因 |
|------|------|------|
| provider（daemon 写 Go backend） | ❌ 作废 | computer-use 是插件不是大脑，不该和 codex 并列 |
| MCP server | ❌ 作废 | 用户明确不要 MCP 服务，保持 CLI 形态 |
| **Skill 教学 + 本机 CLI** | ✅ 采纳 | 最轻，multica 后端零改动，computer-use 保持纯 CLI |

另一事实：**OpenAI 的 Codex Computer Use client 不能拿来用**——`~/.codex/computer-use/` 的 `SkyComputerUseClient`（bundle id `com.openai.sky.CUAService`）是闭源签名预编译二进制，无源码、不可移植。要接的是用户自己开源的 `computer-use-harness`。

### 3.1 核心判断：Skill 教 agent 调本机 CLI

调研三条硬约束定了方向：

1. **skill 不能打包二进制**：`skill_file.content` 是 TEXT（migration 008），写入权限固定 `0o644` 不可执行。computer-use 含 Node CLI + Swift 编译 helper，**塞不进 skill_file**。
2. **agent 能自由执行 shell 调本机 CLI**：Claude Code 无沙箱；Codex 在 macOS 降级到 `danger-full-access`（`codex_sandbox.go:15-31`）。agent 可直接调 PATH 里的 `computer-use` 命令。
3. **skill = 写进 provider-native 目录的 SKILL.md**：daemon 注入到 `.claude/skills/{name}/`、codex-home/skills 等（`context.go:152-214`），provider 自动发现。

**结论：computer-use 本机预装（在 PATH），multica 侧用一个 skill（SKILL.md）教 agent 怎么调它。** skill 不打包二进制，只打包「使用说明书」。

### 3.2 集成形态

```
computer-use-harness  →  本机预装（在 PATH，开源 CLI，保持纯 CLI 形态）
                              ↑ 随 desktop 安装时一起装（呼应端形态：桌面优先）
multica skill (SKILL.md，纯文本)
   "本机有 computer-use 命令。操作本机 UI 时这样调：
    computer-use <action> ...  → 输出 JSON（含 ActionResult / Trace）"
        │ daemon 注入到 .claude/skills/computer-use/（context.go，0o644 文本足够）
        ▼
agent（codex / claude-code）读到 skill → 用自带 shell 工具执行 computer-use CLI
        └─ computer-use 内部：Capability 链 + Policy + Trace + Swift mac-helper
```

**改动量**：multica 后端**几乎零改动**——skill 机制现成（含 `local_skills.go` 本机 skill 自动导入）。主要工作是写好这份 SKILL.md（教学内容 + 调用范例）。computer-use 保持纯 CLI，不加任何外壳。

### 3.3 收尾确认（已定）

1. ~~上 PATH 方式 + SKILL.md 归属~~ → **已定：用户手动装 computer-use CLI + SKILL.md 作为 workspace skill 手动挂到 agent**。最可控，不依赖打包/自动导入机制。
2. skill 内容粒度：SKILL.md 教 agent 怎么调 computer-use（含调用范例 + 输出 JSON 说明）。进 plan 时定详略。
3. policy / 权限：computer-use 自带 policy guard + macOS 权限；SKILL.md 注明权限前提。
4. trace 回流：先留本地（`.computer-use/traces/`），是否接右侧状态栏「来源区」留需求二做完后再议。
5. 非 mac 降级：SKILL.md 注明仅 macOS。

> 含义：需求三 multica 侧改动几乎为零——不打包、不碰 local_skills 自动导入，就是「写一份 SKILL.md + 用户把它建成 workspace skill 挂到 agent」。computer-use CLI 由用户自行装到 PATH。

### 3.4 第一里程碑（最小可验）

用户装好 computer-use CLI（PATH）→ 建一条 computer-use workspace skill 挂到 agent → 该 agent（codex 或 claude-code）在对话中读到 skill，用 shell 调 `computer-use` 拿到 JSON 结果。multica 后端不新增 provider、不改 skill 表结构、不改 schema。

### 3.5 落地状态（2026-06-09）

**multica 侧零代码改动**（如设计预期）——skill 机制现成，需求三的产出是一份 SKILL.md。已落地 `computer-use-harness/SKILL.md`（教 agent 调本机 CLI，frontmatter `name: computer-use` + `description`，符合 multica workspace skill 格式）。

**关键现实校正（摸了真实 CLI 后）**：computer-use-harness 的 CLI 是**用例驱动**，不是自由原子命令。命令面只有 `version / apps / capabilities / usecases list|dry-run|run / trace`——**没有** `computer-use click x y` 这种命令。原子动作（click/type/key/scroll…）是预定义用例（`usecases/cases.yaml`）里的步骤，agent 通过「跑用例 + 读 trace」驱动，要做新动作得先在 YAML 加用例。3.4 原写的「open app + click」据此修正为「跑用例拿 JSON」。能力在（mac-helper 支持这些 JSON-RPC），但 CLI 没暴露成独立命令——给 CLI 加原子命令是 computer-use-harness 自己的增强，不属于 multica 需求三。

SKILL.md 内容已对真实 CLI 逐条验证（UC-001 权限检查 passed、`--fake`+`--mac-helper` → `INVALID_RUN_MODE`、未知用例 → `UNKNOWN_USE_CASE`），输出 JSON 形状、错误码、安装/构建、macOS 权限前提、平台限制均属实。

**如何挂到 agent（用户操作）**：
1. 在 computer-use-harness repo：`npm install && npm run build`（+ `npm install -g .` 上 PATH），原生执行再 `cd native/mac-helper && swift build`。
2. macOS 授予 Accessibility + Screen Recording。
3. 在 multica 把 `SKILL.md` 内容建成一条 workspace skill（Skills 页或 API `CreateSkill`），挂到要操作 UI 的 agent。
4. 该 agent 对话中读到 skill，用 shell 调 `computer-use`。

详见 `memory/2026-06-09-computer-use-skill.md`。

---

## 四、端形态：desktop 拉起本地 server（路径 C，方案级）

### 4.1 核心判断（修正之前的错误）

**之前 layout 写「约 3 行 config」是错的**：本地 daemon 不是 API server（只有 `/health`、`/shutdown`、`/repo/checkout`，见 `health.go:115-191`），业务 API 只在中心 server。所以 desktop 单向连本地 = 让本地有一个完整 server 在听。

### 4.2 路径 C 方案

```
desktop 主进程（daemon-manager 已能 spawn/管理进程）
  └─ 启动 multica server --local-mode --port <动态>
       （而非/或并存 daemon）
desktop 渲染进程 apiUrl → http://127.0.0.1:<port>
daemon 保持纯 executor 角色不变
```

改动约 100-200 行：
- `server/cmd/server/main.go`：加 `--local-mode` / `--local-port` flag（数据源、离线策略）。
- `apps/desktop/src/main/daemon-manager.ts`：增「拉起本地 server」逻辑（沿用现有进程管理）。
- `apps/desktop/src/shared/runtime-config.ts`：apiUrl 指向本地 server。

### 4.3 收尾确认（已定）

1. ~~数据源~~ → **已定：本地 server 仍连中心 DB**。本地 server 只是把 API 端点收到桌面端，数据走中心库——先解决「不依赖浏览器」这个核心诉求，不追求真离线（无本地 DB 部署 + 数据同步的复杂度）。
2. 本地 server 与 daemon **并存**：daemon 仍跑以执行本机任务，本地 server 承接业务 API。
3. 进 plan 时定：`--local-mode` 具体读哪些配置（中心 DB 连接串、token）。

---

## 执行顺序（已定：一 → 二 → 三）

```
1. 需求一（可执行）— 接通 runtime 选择，验证整条「会话→runtime→任务」链
2. 需求二 — 在需求一对话上加 goal 模式 + 编排（最大块，新建 goal_run/goal_subtask；见 2.8 翻转）
3. 需求三 — 给 agent 挂 computer-use skill（最轻，多在用户侧操作）
   端形态路径 C — 可与需求一并行或穿插（让功能跑在桌面端）
```

**实现进度（2026-06-09）**：需求一已合并 main；需求二端到端可用（2.8–2.11）；需求三、端形态路径 C 尚未实现，列为下一阶段候选。

## 与 harness 协议的取舍（本任务）

- **采纳**：双契约（需求一已写）、可执行验收契约。
- **本次先不做**：evaluator 复跑打回循环、三层记忆完整流程——按已定「先 MVP，不做完整 harness 协议」。
- 角色定义双层模型、repo→平台单向同步：作为需求二的基础设施，在需求二 plan 阶段展开（本设计聚焦执行链路）。
