# 设计方案：multica = harness 任务环节的跨工程管理工具

> 状态：**已落地**（2026-06-11）。② goal_persist + ③ 双契约（契约=响应、格式=工程方言、prompt 引导对齐不内置模板）+ ④ goal_decision 后端/前端/测试全绿；worktree 并行 + 执行历史视图留下轮。落地细节见 `memory/2026-06-11-repo-ssot-persist-and-judgment-landed.md` 与 `memory/2026-06-11-contract-is-dialect-of-the-project.md`。本文是定稿的"施工图"。
>
> 解决的问题（用户原话归纳）：
> 1. **框架翻转** —— multica 不是任务模型的发明者，而是 `dev-roleplay-harness` 这套工程哲学的**管理工具**。它跨工程管理：每个工程看到①有哪些角色②哪些任务在跑③执行历史，并驱动 agent 在 repo 里按 harness 的任务环节干活。
> 2. **关键资料文档化** —— 除聊天流外，需要文档化的任务资料（目标、施工图、双契约、决策、memory）应能**一键按 harness 结构沉淀到 repo**，让任意工具/终端开 repo 就能接力。
> 3. **执行模型修正** —— 子任务可能成功也可能失败，主会话（总控）在依赖边上做的是**「下一步判断」**，不是无脑往下派，也不是哑数据管道。

## 〇、先读这些（上下文锚点）

- 上层母本 `~/workplace/opensource/dev-roleplay-harness/`：
  - `doctrine/00-dual-ssot.md` —— 代码存 what / 文档存 why，双 SSOT，知识是仓库一等公民。
  - `doctrine/03-dual-contract.md` —— 施工契约 + 验收契约同文件、互相对应。
  - `doctrine/05-memory-layering.md` —— 三层记忆（按日沉淀 → 任务汇总 → 跨任务知识），决策可分辨性。
  - `roles/*.md` —— 七角色（task-designer / coder / evaluator / code-reviewer / doc-refresher / dreamer / git-push）。
  - `examples/task/260601-todo-tag-filter/` —— 一个任务目录的完整真实样例。
- 实例工程 `~/workplace/ai/AI-GAME/`：`docs/task/260521-playable-snake-evolution/`（progress.md + plan/step-*.md 双契约 + memory/）、`.claude/agents/`（角色）、`.claude/worktrees/`（11 个并行隔离）。
- 记忆 [`two-layer-roles-and-repo-ssot`](memory/2026-06-08-two-layer-roles-and-repo-ssot.md) —— repo 是 SSOT、平台是投影（本文把它从"角色"扩展到"任务"）。
- 记忆 [`task-mode`](memory/2026-06-10-task-mode.md) —— 当前独立任务页 + PMO + 动态小队。
- 设计 [`design-member-orchestration`](design-member-orchestration.md) —— 角色同步（`.claude/agents/` → workspace Agent），本文的姊妹篇：那篇同步**角色**，本文沉淀**任务**，两者是"multica 适配 harness repo"的一体两面。
- 设计 [`design-task-mode.md`](design-task-mode.md) —— 当前交互模型（讨论 → 确认 → 规划 → 执行）。

## 一、框架翻转（这是全文的定盘星）

之前的心智：**以 multica 为中心**，goal_run/goal_subtask DAG 是任务模型本体，"顺便写点东西到 repo"。

翻转后的心智：**以 harness 工程为中心**。

> **multica = 跨工程的多 Agent 任务管理工具。** 每个"工程" = 一个挂载的 harness repo。工具的全部职责是**适配 harness 的结构**，让你在一个界面里管理多个工程——看每个工程有哪些角色、哪些任务在跑、历史怎样——并驱动 agent 在 repo 里按 harness 的任务环节干活。**该文档化的关键资料按需沉淀到 repo（结构化投影），聊天和实时调度态留平台。**

推论（改变了之前的若干结论）：

1. **"适配"是关键动词。** 难点不是发明任务系统，是**正确读懂并对齐 harness 的约定**（任务目录格式、角色文件格式、worktree 布局、memory 分层）。repo 怎么组织，multica 就怎么呈现和驱动。
2. **角色同步 + 任务沉淀 = 同一件事的两面。** 都是 multica 投影/适配 harness repo 的结构。`design-member-orchestration` 做了角色这一半（读 `.claude/agents/`），本文做任务这一半（读写 `docs/task/`）。

## 二、SSOT 边界（最容易做错，先切干净）

**平台 DB 是任务的主真相；repo 沉淀是按需的、快照式的结构化投影。** 不是"内容天然归 repo"——是"内容天然在 DB，repo 是按需导出的一份 harness 结构"。

| 维度 | 主真相 | 内容 | 理由 |
|------|--------|------|------|
| **聊天流** | **DB** | 与总控的讨论对话 | 高频、过程噪声，不该污染 repo 版本史 |
| **运行态/调度** | **DB** | status、assignee、depends_on、attempt、谁在跑、queued/running | 实时、高频、WS 广播、多端同步；调度引擎的活真相 |
| **任务内容** | **DB（默认）→ repo（一键沉淀后）** | 目标、拆解、双契约、里程碑、决策、memory | 默认活在平台（平台自洽，不依赖 repo 也能完整跑）；用户点「持久化到工程」后，按 harness 结构写一份快照进 repo，供其他工具接力 |

**关键判断（务必记住）：**

- **平台自给自足。** 没挂 repo 的工程、没点沉淀的任务，平台就是唯一真相，照常完整跑。repo 沉淀是**增益**，不是前提。
- **"repo 是 SSOT" 只在沉淀之后、且要用其他工具接力那个任务的场景下成立。** 对没沉淀的任务，平台 DB 是唯一真相。这比"强行让所有任务都背 repo 的重量"诚实。
- **一键持久化 = 快照式**（可重复点，每次覆盖/刷新 repo 里那份）。**不做双向同步**——避开"repo 被别的工具改了要回流"的所有坑。点一次写一次；任务在平台继续变，repo 不自动跟，除非再点。
- **server 永不碰 repo。** server 可能远程，摸不到用户的 repo。所有 repo 文件读写都由 **agent 在 daemon 所在机器**上做（daemon 跑在 repo 本地）。沉淀 = 派一个"沉淀任务"给总控 agent，由它在 repo 里 author 文件。

## 三、要做的事（从 harness 结构倒推，5 件）

### ① 工程 = repo 适配层（复用现有，少量扩展）

- 复用 `project` + `local_directory` resource（`goal_run.project_id` 上一轮已加，migration 116）。
- multica 读这个 repo 的两处：
  - `.claude/agents/`（角色）—— **已做**（`RoleSyncService`，见 member-orchestration）。
  - `docs/task/`（任务 + 执行历史）—— **本文新增**（见 ⑤）。

### ② 一键持久化到工程（核心新增）

任务页常驻按钮「**持久化到工程**」：

- **位置/时机**：任务页常驻入口，任何阶段都能点（讨论中 / 执行中 / 完成后）。最常用是**完成后**点一次沉淀成品。
- **默认关**：小任务/临时任务是多数，默认不沉淀。
- **gating**：仅当任务绑定的工程挂了 repo（local_directory）时可用，否则置灰。
- **机制**：点击 → 派一个 `goal_persist` 任务给总控 agent → agent 在 repo 的 `docs/task/{YYMMDD-slug}/` 按 harness 结构 author：
  - `progress.md` —— 目标 + 步骤 checklist（里程碑粗粒度）+ 决策记录表。
  - `plan/step-*.md` —— 每个 subtask 一份**双契约**（施工契约 + 验收契约，同文件）。
  - `memory/YYYY-MM-DD-*.md` —— 若讨论/执行中有决策沉淀。
- **快照覆盖**：重复点 = 用当前 DB 状态重新生成那份目录（覆盖刷新），不做增量 diff。

### ③ 施工图工作流（规划态产出双契约）

总控规划时，产出的不只是扁平节点列表，而是 harness 式**施工图**：

- 讨论态：总控引导澄清 → 收敛出目标（progress.md 的目标段，即之前说的"任务卡"）。
- 规划态：总控产出完整施工图——每个节点带**施工契约**（给执行 agent：可改文件/不可改/产出清单/约束）+ **验收契约**（给 verify 节点：可机器判定的检查项）。
- **施工图先行**：先把整张图画完、定死，再并行铺开，不是小切片迭代。
- 落地（已做，2026-06-11 补齐）：**契约是 agent 的响应,不是 DB 字段**——subtask `spec` 自由文本就承载双契约,零 schema 改。**格式属于工程方言,不是 multica 内置模板**:规划/持久化 prompt 只给双契约思路 + 引导 agent 先读本工程 `docs/task/*/plan` 既有契约对齐方言(无则退化到通用形状),绝不焊死标题。前置地基:planning/subtask 任务带上 `ProjectID` + claim surface `ProjectResources`,让 agent 跑在工程 repo 里才能参照方言。详见 `memory/2026-06-11-contract-is-dialect-of-the-project.md`。

### ④ 主会话「下一步判断」（执行模型实质改动）

现状：A 完成 → `handleSubtaskTerminal` 自动 ready → 派 B，**中间无判断**（仅 verify 节点例外）。

改成：**只在失败 / 部分成功 / 高风险边**插总控判断节点：

- A 成功且低风险 → **自动放行**派下游（现有逻辑不动，省 token）。
- A 失败 / 部分成功 / 总控规划时标了"关键边" → **派一个判断任务给总控**：评估上游产出 → 决定下游推进 / 塑形 spec / 中止返工。
- 传的是**判断**，不是数据，也不是 memory 黑板（memory 是按日时间沉淀，不是节点交接介质——这是明确否掉的方案）。

落地：在 `handleSubtaskTerminal` 的"成功→自动 ready 下游"分支前，加一个"是否需要总控判断"的岔口；需要时派 `goal_decision` 任务给总控，由它经 CLI（`multica goal decide` 类比 `goal verdict`）回写"放行/塑形/中止"。

### ⑤ 执行历史视图（读 repo 呈现）

- multica 读挂载工程的 `docs/task/*` 目录，呈现该工程的**历史任务**（progress.md 的目标 + 步骤完成度）。
- 不依赖 DB 留存——这正是 repo-SSOT 接力的价值：换工具/换机器，从 repo 就能看到这个工程做过哪些任务。

### 改名（保留"总控"）

- **不改 PMO → task-designer。** 沿用之前定的 **Coordinator / 总控**（`resolveCoordinatorAgent`、"总控正在规划…"），纯文案/标识符 ~50 处，无逻辑改动。
- 理由：用户明确要求保留"总控"这个词。harness 的 task-designer 是参照，不是强制改名。

### 本轮不做（留下一轮）

- **worktree 并行**：harness 的 `.claude/worktrees/` 约定成熟（AI-GAME 跑过 11 个），随时能接。本轮先单 worktree 串行跑通 repo 沉淀 + 判断模型；并行单独成轮，避免一口吃成夹生饭。

## 四、做事方式（铁律，落地时不容破）

- **后端永不调 LLM。** 所有 AI 动作 = 派任务给 agent + CLI 回写：施工图生成 = 派规划任务给总控；下一步判断 = 派判断任务给总控；repo 沉淀 = 派沉淀任务给总控。沿用现有 `goal plan` / `goal verdict` 模式。
- **server 不碰 repo。** 所有 repo 文件读写由 agent 在 daemon 机器上做。
- **布局遵 harness 约定，不另造。** `docs/task/{id}/`、`progress.md`、`plan/step-*.md` 双契约、`memory/` 分层、worktrees——照搬 harness，不发明 multica 专属格式。
- **DB ↔ repo 不重复真相。** DB 是主真相；repo 是快照投影。同一份内容不在两处都"活"——repo 那份是某一时刻的导出，明确允许过时（直到再次点沉淀）。
- **施工图先行。** 先出完整设计定死，再分工，再执行（这也是本次干活方式）。
- **凭证安全。** shell 里有真实凭证（Bedrock/Azure/OpenAI relay）——绝不写进代码/文档；提交前扫一遍 docs。

## 五、测试用例（按"改动本身"验收，不死等 LLM 真写完）

> 沿用上轮 token 教训：验证到"机制跑通"即可，不等 LLM 把示例任务真做完。

| # | 用例 | 通过标准 |
|---|------|---------|
| 1 | **一键沉淀写 repo** | 点「持久化到工程」→ repo 出现 `docs/task/{id}/progress.md` + `plan/step-*.md`，含目标 / 步骤 checklist / 双契约。Go 单测覆盖 CLI 回写路径（含 malformed 输入 fail-closed）。 |
| 2 | **快照可重复** | 任务状态变化后再点一次 → repo 那份被覆盖刷新为最新，不产生重复目录。 |
| 3 | **接力** | 另一工具/终端直接读该 repo `docs/task/{id}/` → 拿到完整任务上下文（目标 + 施工图 + 里程碑），**不依赖 multica DB**。 |
| 4 | **gating** | 未挂 repo 的工程，「持久化」按钮置灰；挂了 repo 才可点。 |
| 5 | **下一步判断 - 失败路** | 构造上游 A 失败 → 验证派出总控判断任务，**不自动派 B**；总控决定中止/返工。 |
| 6 | **下一步判断 - 成功路** | 上游 A 成功且低风险 → 自动放行派 B（现有逻辑），**不**多派判断任务（不浪费 token）。 |
| 7 | **平台自洽** | 没挂 repo / 没点沉淀的任务，全程在平台正常跑（讨论→规划→执行→收口），不因缺 repo 报错。 |
| 8 | **改名回归** | 全链路文案/标识符无残留 PMO（保留"总控"），i18n 4 语言齐。 |

## 六、与现有代码的衔接点（落地时的锚）

- `server/internal/service/goal.go`
  - `handleSubtaskTerminal`（:1436）—— ④ 判断岔口插这里（成功→自动 ready 前）。
  - `SubmitPlan` / `persistSubtasks`（:602 / :654）—— ③ 双契约结构承载在 subtask spec。
  - 新增 `goal_persist` / `goal_decision` 任务派发（类比 `dispatchPlanningTask` :555 / `maybeDispatchSummary` :1586）。
- `server/internal/daemon/prompt.go`
  - 新增 `buildGoalPersistPrompt` / `buildGoalDecisionPrompt`（类比 `buildGoalPlanningPrompt` :52）。
- `server/cmd/multica/cmd_goal.go`
  - 新增 `multica goal decide <subtask> <proceed|reshape|abort>` + repo 沉淀走 agent 自身文件写（无需新 CLI，agent 直接在 repo author）。
- 前端
  - 任务页常驻「持久化到工程」按钮 + gating（读 project 是否挂 local_directory）。
  - 执行历史视图（读 repo `docs/task/*`，或经 server 代理读）。
- 复用 `RoleSyncService.resolveProjectDir`（member-orchestration）解析 repo 路径。

## 七、开放/待落地时再定（不阻塞本设计定稿）

- repo 沉淀的目录命名：`{YYMMDD}-{slug}`，slug 从标题 kebab 化——与 harness/AI-GAME 一致。
- 双契约在 DB 的承载：复用 `spec`（塞结构化 markdown）还是新增列——落地时按最小改动定。
- 「下一步判断」的"高风险边"如何标记：总控规划时在 subtask 上打 flag，还是按 kind 推断——落地时定。
- 执行历史读取：daemon 代理读 repo 还是 server 直读（单机可直读，远程要走 daemon）——落地时按部署形态定。
