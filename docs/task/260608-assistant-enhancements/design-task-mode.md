# 任务模式（Task Mode）设计：独立入口 + PMO 规划层 + 动态小队 + 四栏

> 任务 260608 ｜ 2026-06-09 ｜ 升级自 design-goal-ui.md
> 起因：实机 dogfood 后用户反馈三个偏差——目标模式不该寄生助理页、不该选固定 squad、布局要主/子会话输出流。本文固化新交互模型，后端执行引擎不动。

## 一、三个偏差 → 三个修正

| 偏差（旧实现） | 修正（本设计） |
|---|---|
| 目标模式是助理页的一个模式开关 | **独立顶级页「任务」**，导航单独入口，与「助理」（单 agent 聊天）分开 |
| 创建时选一个预建固定 squad | **PMO 规划层 + 一个个挑成员组合**；选完动态建「XXX 目标小队」 |
| 右侧只有状态树 | **四栏**：③讨论+成员+状态树，④主/子会话实时输出流 |

## 二、核心概念

### PMO = 最外层模型规划/交互层（非预定义成员）

- PMO 是你在③上多轮对话的那个模型，负责规划任务、分配角色。
- **默认来源（已定）：workspace 级默认规划 agent**。workspace 设置里指定；没指定就用一个内置回退（workspace 第一个可用 agent）。需要时允许按任务覆盖（后续）。
- 讨论的「主会话」= 跟 PMO agent 的一个 chat session（复用现有 chat 机制）。

### 成员 = 目标模式里一个个挑 agent 组合（先做手动，PMO 可引导补角色）

- 默认手动勾选 workspace 的 agent 作为本次成员。
- PMO 在讨论中可建议补缺失角色（对抗角色 / code review 角色等），引导用户创建。
- 不再"选一个预建固定 squad"。

### 动态小队 = 执行时产物（非预建）

- 选完成员，执行时**后端自动建一个动态 squad**「XXX 目标小队」：`leader_id = PMO 规划 agent`，`members = 选的 agent`。
- 复用先不做。这让现有 `goal_run.squad_id` + 调度引擎**零改动**——动态 squad 就是普通 squad。

## 三、四栏布局

```
┌────────┬─────────────┬──────────────────────────┬────────────────────────┐
│ ① 导航  │ ② 任务列表   │ ③ 讨论 + 成员 + 状态树     │ ④ 主/子会话输出流        │
│        │ (goal 列表) │                          │                        │
├────────┼─────────────┼──────────────────────────┼────────────────────────┤
│ 收件箱  │ [+ 新建任务] │ ┌ 上: 与 PMO 多轮讨论 ──┐ │ ┌ 主会话(PMO 编排) ──┐ │
│ 我的    │             │ │ 你: 重构登录模块      │ │ │ PMO: 已拆为 3 步...   │ │
│ Issues │ ▸ 重构登录   │ │ PMO: 验证码走短信吗?  │ │ │ 正在派发子任务 1...   │ │
│ 项目    │   executing │ │ 你: 邮件             │ │ │ (实时流,像 CC 主会话)│ │
│ 自动化  │ ▸ 起名任务   │ │ [成员: ✓Coder ✓Rev] │ │ └────────────────────┘ │
│ 智能体  │   completed │ │ [+ PMO 建议加对抗角色]│ │                        │
│ 小队    │             │ │ [确认执行]           │ │ 点③下子任务 → ④切到    │
│ ★任务   │             │ └──────────────────────┘ │ 该子会话的执行输出       │
│ 助理    │             │ ┌ 下: 任务状态树(DAG) ─┐ │                        │
│ 配置... │             │ │ ◆ Goal              │ │                        │
│        │             │ │ ├ ✓子任务1 (点→④)   │ │                        │
│        │             │ │ ├ ◌子任务2 (点→④)   │ │                        │
│        │             │ │ └ ○子任务3          │ │                        │
│        │             │ └──────────────────────┘ │                        │
└────────┴─────────────┴──────────────────────────┴────────────────────────┘
```

- **③上 讨论**：和 PMO 多轮对话确定目标 + 勾成员 + PMO 引导补角色 → [确认执行] 闸门。
- **③下 状态树**：子任务 DAG，每个节点是"切进子会话"的入口（点节点 → ④切到该子会话输出）。
- **④ 输出流**：默认主会话（PMO 编排实时流，类比 Claude Code 主 lead 会话）；点③下子任务 → 切到该子任务执行 agent 的子会话输出。

## 四、三阶段（讨论 → 确认 → 执行）

1. **讨论**：创建任务 → 和 PMO 多轮对话收敛目标 + 选成员 + PMO 引导补角色。④显示讨论主会话。goal 状态 `discussion`。
2. **确认**：[确认执行] 闸门。后端：动态建「XXX 目标小队」(leader=PMO, members=选的) → PMO 拆解 DAG。
3. **执行**：现有引擎接管（DAG 调度 / 对抗验证 / 干预 / 接管）。③下树实时更新，④主会话流 + 可切子会话。

## 五、什么留、什么改

| 层 | 状态 |
|---|---|
| goal_run / goal_subtask / DAG / 调度 / verify / 干预 / 接管 | ✅ 全留，零改动 |
| 动态 squad（leader=PMO, members=选的） | 🆕 创建时机从"预建"→"确认执行时自动建" |
| `goal_run.squad_id` | ✅ 仍用，但指向动态建的小队（非用户预选） |
| workspace 默认规划 agent（PMO 来源） | 🆕 workspace 设置加字段 + 回退逻辑 |
| 讨论阶段主会话 | 🆕 复用 chat session（绑 PMO agent），讨论收敛产出目标 |
| 独立「任务」页 + 导航入口 | 🆕 从助理页剥离 |
| 四栏布局（③讨论+状态、④主/子会话输出流） | 🔁 重构 GoalPanel |
| 助理页目标模式开关 | ❌ 删 |

## 六、数据模型改动（最小）

- **`workspace` 加 `default_planner_agent_id`**（nullable，PMO 来源；空时回退 workspace 首个可用 agent）。
- **`goal_run.squad_id`** 语义不变（指向小队），但小队是确认执行时动态建的。无需改表。
- 讨论主会话：复用 `chat_session`，可加 `goal_run_id` 关联（类比已加的 `goal_subtask_id`），让讨论 chat 知道"我属于哪个 goal"。
- 其余表全部不动。

## 七、分层实现顺序

1. **L0**：workspace.default_planner_agent_id 迁移 + chat_session.goal_run_id（讨论会话关联）+ sqlc。
2. **L1 后端**：解析 PMO（workspace 默认+回退）；创建 goal（discussion 态，建讨论 chat）；确认执行时动态建小队 + 派 PMO 规划；成员从请求传入。
3. **L2 前端**：导航加「任务」入口 + 独立任务页；四栏（讨论 chat + 成员选择 + 状态树 + 主/子会话输出流）；删助理页目标开关。
4. **验证**：端到端 case——建任务 → 和 PMO 讨论 → 选成员 → 确认 → 看拆解执行 → 切子会话；用 computer-use 驱动 Multica 客户端走一遍。

## 七点五、落地状态（2026-06-10，已实现 + 实机验证）

全部分层落地，PMO=workspace 默认规划 agent（回退首个可用），实机端到端验证通过。

| 层 | 落地 |
|----|------|
| L0 | migration 115（workspace.default_planner_agent_id + chat_session.goal_run_id）；queries（SetWorkspaceDefaultPlanner / CreateDiscussionChatSession / SetGoalRunChatSession / ListGoalRunsForWorkspace）|
| L1 | service `CreateTask`/`AddTaskMember`/`ConfirmTask`/`resolvePlannerAgent`；handler `/api/tasks`（GET/POST/PUT planner）+ `{id}/members|confirm`；动态建小队（leader=PMO + 成员）|
| L2 | 独立 TasksPage 四栏（任务列表 / 讨论 chat + 成员勾选 + GoalStatusTree / 主子会话输出）；nav 加「任务」；paths + reserved_slugs；删助理页目标开关 |
| 测试 | Go：TestTaskMode*（discussion+动态小队 / confirm→planning / addMember）；前端 TasksPage 组件测试（创建表单+成员选择+列表）|

**实机验证**：API 驱动建任务→discussion→动态小队→确认→planning→真 claude PMO 拆出 3 节点链式工作流（execute Hello Bot → **verify E2E Agent** → execute Hello Bot），goal completed。computer-use 驱动桌面确认「任务」nav 入口独立 + TasksPage 渲染。详见 `memory/2026-06-10-task-mode.md`。

验证：views 1091/1091 + core 460/460 + 6 包 typecheck + lint 全绿 + 全 Go goal/task 套件通过，零回归。

## 七点六、④ 输出流落地（2026-06-10 第二轮，dogfood 反馈四连修）

实机用 ④ 栏后用户连发四个问题，全部已修 + 实机验证。详见 `memory/2026-06-10-execution-output-visibility.md` 与 `memory/2026-06-10-pmo-summary-closeout.md`。

| 反馈 | 根因 | 修复 |
|------|------|------|
| 只看到"执行成功"状态，看不到过程/结果 | API 丢了 task_id/planning_task_id/result | `enrichGoalResponse` 喂三字段；`TaskStream` 拉 task_messages |
| 段落分不清、要 markdown | 第一版 ④ 是 raw `<pre>` JSON，无思考/结果分区 | **复用** chat 的 `TimelineView`（preface 文本 · 折叠过程 · markdown 最终答案），抽到 `common/task-transcript/`；删平行渲染器 |
| 右下角气泡入口要删 | — | 卸载 `ChatFab`+`ChatWindow`（Fab 是浮窗唯一入口，一起删；web+desktop 两处布局）|
| 拉到底也读不全 | 页根 `h-screen`(100vh) 忽略顶栏/Tab 条 | 改 `h-full min-h-0`，②③④ 列 wrapper 全补 `min-h-0` |
| 子任务有输出，主任务（PMO）无最终结果 | PMO 规划任务一次性（规划→派发→结束），无收口 | **PMO 收口/汇总**：见 §七点七 |
| 规划/已完成子任务 stream 实时为空（数据在 DB） | `ReportTaskMessages` 用自己的 issue/chat-only 工作区解析，goal 任务解析出 `""` → `task:message` WS 广播被跳过 | 换成共享 `ResolveTaskWorkspaceID`（**铁律：任何解析任务工作区的 handler 都用它**）|

## 七点七、PMO 收口/汇总（goal_summary，2026-06-10）

设计文档原意 PMO 职责含"汇总"，但只实现了规划+派发。补一个 **summary 任务**做收口（零 schema 改动）：

- 新 context 类型 `goal_summary`（镜像 `goal_planning`）。
- `recomputeGoalStatus`：所有子任务终结**且有成功项**时，先派一个 summary 任务给 PMO（读全部子任务产出 digest → 写最终交付），goal 留在 `executing`；`GetSummaryTaskForGoal` 幂等防重派。summary 任务完成 → `SyncSummaryFromTask` → `finalizeGoalRun` 收口（completed/partial）。无成功项（全 failed）则不派、直接 finalize。
- daemon `buildGoalSummaryPrompt`：合成各子任务产出成最终交付，**无 CLI 回写，回复即结果**。
- API 暴露 `summary_task_id`；④ 主会话渲染 planning 流 + summary 流（带"最终交付"分隔）。
- `goalContextWorkspaceID`（原 `goalPlanningWorkspaceID`）同时认 planning + summary，否则 summary 流 WS 广播又被丢。

测试：`TestGoalDAGHappyPath` 驱动完整 summary 循环；`TestGoalPlanningWorkspaceID` 加 summary 用例；`TestReportTaskMessagesBroadcastsForGoalTask` 锁 WS 广播。实机：真 claude 跑通 plan→execute→verify→**summary**，PMO 产出最终交付（"天空蓝是因为瑞利散射…"），UI 实测可见。

**后续候选**（非阻塞）：讨论中 UI 层"加成员/PMO 引导补角色"按钮（后端 AddTaskMember 已就绪，前端未 surface）、computer-use keyword 绑定驱动完整 GUI 流。

## 八、与既有 memory 的衔接

- 执行引擎、对抗验证、干预、接管：见 `memory/2026-06-09-dynamic-workflows-verify-nodes.md`、`failure-intervention.md`、`human-takeover.md`，全部不变。
- 后端不调 LLM、派任务给 agent 的约束：`llm-decompose-via-leader-task.md` 仍成立（PMO 规划 = 派任务给 PMO agent）。
- 本设计取代 design-goal-ui.md 的"选固定 squad"和"助理页模式开关"，其余对齐。
