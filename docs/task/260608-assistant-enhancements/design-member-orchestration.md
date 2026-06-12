# 设计方案：成员编排 —— 目标 → 工程 → 成员 的串联

> 状态：**方案设计,未写码**（2026-06-10 对焦）。决策已拍板：成员落地形态 **C（识别为建议 + 一键建 Agent）**、任务创建时**选工程**、本轮**只出设计**。
>
> 解决的问题（用户原话归纳）：成员概念被固化——没有独立的成员配置入口（只有「小队」菜单，而小队概念已弱化）；成员不能读所依赖工程的内容做**自识别/生成**（像上层 `dev-roleplay-harness` 的 `roles/` + task-designer）；**目标 → 工程 → 成员**没有串成一条线。

## 〇、先读这些（上下文锚点）

- `requirement.md` §需求二「三层/双层角色模型」「repo 是 SSOT,平台是投影」「导入已有 harness 工程」。
- 记忆 [`two-layer-roles-and-repo-ssot`](memory/2026-06-08-two-layer-roles-and-repo-ssot.md) —— L1 职责（Agent）/ L2 工程规范（repo）/ 组合在派发时；repo→平台单向同步（**承重墙,曾标待拍板**）。
- 记忆 [`dynamic-workflows-verify-nodes`](memory/2026-06-09-dynamic-workflows-verify-nodes.md) —— squad = 可调用角色库（成员池）,非固定团队;PMO 规划=工作流设计。
- 记忆 [`task-mode`](memory/2026-06-10-task-mode.md) —— 动态小队当前实现：创建任务时建「XXX 目标小队」(leader=PMO + 选的成员)。
- 上层 `~/workplace/opensource/dev-roleplay-harness/`：`roles/*.md`（coder/evaluator/code-reviewer/task-designer/…，每个含 Soul/输入契约/工作流/硬边界）+ `docs/engineering/`（L2）。**这是「成员从工程自识别」的母本。**

## 一、现状缺口（代码核对结论）

| 缺口 | 现状证据 | 应该 |
|------|---------|------|
| **① 无成员配置入口** | 成员只能在「小队」菜单管;但小队已弱化为"一次任务的成员集合"。任务流程里无挑/加成员 UI（上一轮"对话即创建"还删掉了创建表单里的成员勾选） | 规划阶段有成员入口：挑现有 Agent + **新建 Agent** |
| **② 成员预建死、不能从工程自识别** | PMO 规划 prompt 的 squad roster 由 claim handler 注入 = workspace 已有 Agent 硬列表（`server/internal/daemon/prompt.go:48`）。Agent 全靠人在「智能体」菜单手工建 | 读所依赖工程内容（`roles/`、代码结构、技术栈）**识别/生成**角色 |
| **③ 目标与成员未串联** | `goal_run` **无 `project_id`**（已核对 schema）;成员=workspace 全量 Agent,与"这个目标需要什么角色"无关 | 目标驱动：目标→依赖工程→读工程→识别角色→选/建→组成本次动态小队 |

## 二、核心数据真相（现状,设计必须吃住）

- **`goal_run` 没有 `project_id`** —— "任务依赖某工程"是新链接,要加列（migration）。
- **`project_resource`**（`resource_type` ∈ `github_repo`/`local_directory` + `resource_ref`）= 工程指向的 repo/目录。"读工程内容"= 读这个 resource 指向的 repo。
- **`agent` 是 workspace 级**（`agent.workspace_id NOT NULL`,无 project 级 agent）。C 方案识别出的角色落成 Agent → 进 workspace 池,跨任务可复用。
- **`agent_template` + `CreateAgentFromTemplate` 已存在**（`server/internal/handler/agent_template.go:149`）—— "一键建 Agent"可复用此机制（角色建议 = 临时模板 → 用户确认 → 建实例）。
- **执行引擎吃 `subtask.assignee_agent_id`（真 Agent UUID）** —— 这是选 C 不选 B 的根本原因：C 全程复用现有引擎,零执行引擎改动。

## 三、目标态：目标 → 工程 → 成员 的串联

```
新建任务（对话即创建）
  └─ 选「依赖工程」(project，带 repo/local_directory)         ← 新增一步
       └─ 讨论阶段：和 PMO 聊目标
            └─ [成员入口]                                      ← 新增
                 ├─ 现有成员池：workspace 已有 Agent，勾选进本次小队
                 ├─ 一键新建 Agent：用 agent_template，建好即进池
                 └─ ⭐ PMO 自识别（读工程 roles/ + 代码结构）   ← 本轮只设计
                      → 产出「建议角色」列表（名字/职责/为什么需要）
                      → 用户逐个确认 → CreateAgentFromTemplate 建成 Agent → 进池
            └─ [确认执行]
                 └─ 本次任务的成员集合 = 动态小队（leader=PMO + 选中成员）
                 └─ PMO 规划：从这一池角色按目标设计工作流（含对抗验证节点）
```

**关键：小队不再是预建实体,而是"一次任务规划出的成员集合"的投影。** 这与现状动态小队实现一致（[[task-mode]]），只是把"成员从哪来"从"手工挑 workspace Agent"升级为"目标 → 工程驱动的识别 + 选/建"。

## 四、分阶段落地（本轮只设计,标注哪些是后续）

### 阶段 1（下一轮可做，前端 + 少量 API）：成员入口 + 手工挑/建

1. **任务关联工程**：
   - 后端：`goal_run` 加 `project_id`（nullable，migration）。`CreateTask` 接受可选 `project_id`。
   - 前端：对话即创建流加一步轻量「选工程」（可跳过 → 退回 workspace 全量 Agent 池）。
2. **成员入口**（合并主窗口里,或挂起树旁）：
   - 看板：本次小队成员（来自 `squad_member`）+ 「加成员」。
   - 加成员 = 从 workspace Agent 池勾选 → `AddTaskMember`（已存在）。
   - 新建成员 = `CreateAgentFromTemplate`/`CreateAgent`（已存在）→ 建好自动 `AddTaskMember`。
3. **去固化**：成员选择从"创建前表单"彻底移到"讨论阶段的成员入口",PMO 可在讨论里建议补人。

### 阶段 2（再一轮，碰 PMO prompt + 工程读取）：⭐ 从工程自识别角色

这是用户要的核心，但**依赖承重墙拍板**（见 §五），且要动 PMO prompt + 工程读取，单独成轮：

1. **读工程**：任务绑的 project → `project_resource`（repo/local_directory）→ 读 `roles/*.md`（若有 harness 结构）/ 代码结构 / 技术栈。
2. **PMO 识别**：派一个"成员识别任务"给 PMO（**仍遵守铁律：后端不调 LLM,派任务给 agent**，同 planning/summary 模式 [[llm-decompose-via-leader-task]]）。PMO 读工程 → 产出建议角色清单（name / soul / 职责 / 为什么这个目标需要它）。
3. **人在回路（C 方案）**：建议角色在成员入口里逐个展示 → 用户确认 → `CreateAgentFromTemplate` 建成 Agent → 进池。**不自动建,避免 Agent 列表失控。**
4. **L1/L2 组合**：识别出的角色 = L1（写进 Agent.instructions）;派发时再叠 L2（工程规范，从 repo `docs/engineering/` 读）—— 复用双层模型，组合在派发时。

## 五、必须拍板的承重墙（阻塞阶段 2）

需求文档 §待死的点 1 早标了「**repo→平台同步方向待最终拍板**」。阶段 2 直接撞上：

- **同步方向**：识别角色读 repo `roles/` 是 `repo → 平台` 单向（强烈倾向，避免双 SSOT 漂移）。但"平台新建的 Agent"要不要回写 repo `roles/`?倾向**不回写**（平台建的 Agent 是 workspace 级复用资产,不是某 repo 的 SSOT;repo 的 `roles/` 只作为识别的输入源）。
- **无 harness 结构的普通 repo**：没有 `roles/` 时,PMO 仅靠代码结构/技术栈识别（弱），或提供"一键初始化 harness 骨架"。
- **project ↔ repo 必须先绑**：阶段 1 的"选工程"要求 project 已挂 `project_resource`;否则阶段 2 无内容可读。

## 六、为什么不选 A / B（决策留痕）

- **B（任务内临时角色）**：最贴合"小队=一次任务成员集合"的直觉,但执行引擎吃 `assignee_agent_id`（真 Agent）。临时角色要么造"幽灵 agent",要么动执行引擎建新成员模型 —— **违反本轮 UI 铁律 + 破坏零改动执行引擎**。否决。
- **A（直接建正式 Agent,无确认）**：复用引擎、最省事,但每个工程识别一批 → workspace Agent 列表膨胀、无人把关质量。否决。
- **C（识别为建议 + 一键建）**：✅ 人在回路把关 + 复用执行引擎（assignee=新建 Agent 的 id,零引擎改动）+ Agent 持久可跨任务复用。**已拍板。**

## 六点五、左侧菜单精简 + 新增「角色」入口（2026-06-10 追加需求）

> 用户：左侧工作区菜单杂乱,**先隐藏 issue / 项目（project）两个概念**,**新增「角色」作为菜单入口**,整个框架围绕当前任务模式的改动重新设计,遗留入口适当精简。

### 现状（`packages/views/layout/app-sidebar.tsx`）

三组导航：
- **personalNav**：收件箱(inbox)、我的 issue(myIssues)
- **workspaceNav**：issues、projects、autopilots、agents、squads、usage、tasks、assistant
- **configureNav**：runtimes、skills、settings

### 目标态（围绕任务模式收敛）

| 菜单项 | 处置 | 理由 |
|--------|------|------|
| **issues** | **隐藏** | issue 概念暂不作为一级入口（任务模式是新主线） |
| **myIssues** | **隐藏** | 同上（依附 issue） |
| **projects** | **隐藏**（作为一级入口） | project 概念退居幕后 —— 但**不删数据/路由**：任务要"选依赖工程"仍需 project+repo（§三/§四），project 变成"任务内选择"而非独立浏览入口 |
| **agents** | **改名/合并为「角色」** | 角色 = Agent（双层模型 L1）。把「智能体」入口重定义为「**角色**」,成为成员的配置/新建中心。这正是用户要的"角色作为菜单入口" |
| **squads** | **隐藏或并入「角色」** | 小队已弱化为"一次任务的成员集合",不再需要独立预建入口（[[task-mode]]）。保留数据,去掉一级入口 |
| **tasks** | **保留（主线）** | 任务模式主入口 |
| **assistant** | 保留 | 单 agent 对话 |
| autopilots / usage / runtimes / skills / settings | 保留 | 与本轮无关,暂不动 |

### 收敛后的 workspaceNav（建议）

```
工作区
  ├─ 任务      (tasks)      ← 主线
  ├─ 角色      (roles)      ← 原 agents 改名/重定义,成员配置中心
  ├─ 助理      (assistant)
  ├─ 自动化    (autopilots)
  └─ 用量      (usage)
配置
  ├─ 运行时    (runtimes)
  ├─ Skills
  └─ 设置      (settings)
```

personalNav（inbox/myIssues）整组隐藏。

### 「角色」入口 = 成员配置中心（串联本设计）

把原「智能体(agents)」入口重定义为「角色」,它就是 §三/§四 缺口①要的**独立成员配置入口**：

- 列出 workspace 的角色（= Agent，L1 职责）。
- 新建角色（复用 `CreateAgent`/`CreateAgentFromTemplate`）。
- 任务规划阶段的"成员入口"从这里取池子 + 可跳转到这里建新角色。

这样三者闭环：**「角色」菜单（池子的家）→ 任务里选工程 → 讨论里挑/建成员（阶段2 可从工程自识别）→ 动态小队 → PMO 规划**。

### 落地注意（精简 ≠ 删除）

- **隐藏用「不渲染入口」实现,不删路由/数据/handler**：issue/project 的页面、API、表都留着（project 还要被任务选用;issue 可能后续回归）。只从 `workspaceNav`/`personalNav` 数组移除条目（或加 feature flag）。
- **reserved-slug 不动**：路由还在,slug 保留。
- **i18n**：`agents` → 新增/复用 `roles` 文案键（4 语言）。
- 这是纯前端改动（菜单数组 + 一个入口重命名）,除"角色页"若要扩展成员管理 UI 外,不碰后端。

## 六点七、同步功能（核心交付）+ 参考工程实测格式（2026-06-10 定）

> 用户定调：**「关联工程时自动把工程里定义的角色同步出来」就是要做的核心功能**。参考工程 = `/Users/sumo/workplace/ai/AI-GAME`（用户指定）。格式"自动探索"。

### 参考工程实测（AI-GAME，2026-06-10 探查）

AI-GAME 里角色定义**两种格式并存**,同步功能的"自动探索"要都认：

1. **`.claude/agents/*.md`（带 frontmatter，Claude Code 标准）** —— 6 个：coder / evaluator / code-reviewer / task-designer / dreamer / doc-refresher。结构：
   ```markdown
   ---
   name: coder
   description: >
     子任务实现者。接收 task-designer 预先规划好的单个独立子任务……
   color: cyan
   ---
   你是 `AI-GAME` 的子任务实现者……
   读取并执行 `agents/coder/coder.md` 中定义的完整实现流程。
   ```
2. **`agents/<role>/<role>.md`（harness 散文，无 frontmatter）** —— 完整角色定义（Soul / 工作流程 / 硬边界）。`.claude/agents/coder.md` 正文**解引用**到这里。

→ **双层引用结构**：`.claude/agents/*.md` 是「门面 + 指针」（name/description/color + 一句引导），`agents/<role>/<role>.md` 是「完整 instructions 正文」。

### 同步解析策略（自动探索的落地规则）

```
1. 优先扫 .claude/agents/*.md
     frontmatter.name        → Agent.name
     frontmatter.description  → Agent.description
     frontmatter.color        → Agent 配色（可选映射）
     正文 + 解引用的 agents/<name>/<name>.md 内容 → Agent.instructions（L1 职责）
2. 回退：无 .claude/agents/ 时,扫 roles/*.md 或 agents/*/*.md 的散文结构
     文件名/一级标题 → name；正文 → instructions
3. 解引用：正文里出现 `agents/<x>/<y>.md` 路径 → 读该文件内容合并进 instructions
```

- **格式契约定案**（需求文档 §待死的点 3）：**同时认 `.claude/agents/*.md`（frontmatter 优先）和 `agents|roles/*.md`（散文回退）**。AI-GAME 恰好两者都有,是绝佳的双格式测试样本。
- **L1/L2 边界**：同步出来的是 **L1（角色职责）**。L2（工程规范）单独从 `docs/engineering/` 读,派发时叠加 —— 不混进同步的 Agent.instructions。
- **承重墙（仍待拍板）**：同步是 `repo → 平台` 单向。平台同步建出的 Agent **不回写** repo（repo `agents/` 是 SSOT 输入源,平台 Agent 是 workspace 复用资产）。

### 同步触发点

- **关联工程时自动同步**（用户原话）：project 绑定 `project_resource`（local_directory=AI-GAME 路径 / github_repo）→ 触发一次扫描 → 建/更新 Agent。
- 幂等：按 `name` 去重,已存在则更新 instructions（或提示用户 diff 确认,呼应 C 方案人在回路）。

### 验证路径（用 AI-GAME dogfood）

1. 在 multica 建/选一个 project,挂 `local_directory` 资源指向 `/Users/sumo/workplace/ai/AI-GAME`。
2. 触发同步 → 平台扫到 6 个 `.claude/agents/*.md` → 建成 6 个 Agent（coder/evaluator/code-reviewer/task-designer/dreamer/doc-refresher），instructions = frontmatter 正文 + 解引用的完整流程。
3. 新建任务,选这个 project 作依赖工程 → 成员池自动有这 6 个角色 → PMO 规划时按目标选用 → 端到端执行 → 收口。
4. 真机（computer-use）截图复核：角色入口列出 6 角色、任务规划用上、状态树/④ 流正常。

## 六点六、实施与验证策略（2026-06-10 追加，dogfooding）

> 用户：先形成整个方案,**之后用「目标模式」自己来落地这套方案并端到端验证**（dogfooding）。验证时,把参考 dev-roleplay-harness 设计的那些角色**提前预制出来**,或用同步功能在关联工程后自动创建,方便功能验证。

### 实施策略：用任务模式落地任务模式

整套方案（阶段 1 + 阶段 2）作为一个 goal 喂给当前工程的 PMO,让任务模式自己拆解、分派、执行——既落地功能,又是对任务模式本身最真实的 dogfood。前提：本工程已绑 repo（`multica-sumo` 本身），角色池就位。

### 验证角色的来源（关键事实核对）

- ❌ **「repo→平台 角色同步」功能现在不存在**（grep 无任何读 `roles/*.md` / import harness 的实现）。所以"用同步功能自动建角色"**本身就是阶段 2 要造的东西**,不能拿它当阶段 1 的验证前提。
- ✅ **平台已有 25 个内置 agent template**（`server/internal/agenttmpl/templates/*.json`），含 `code-reviewer`、`bug-fixer`、`frontend-builder`、`prd-critic`、`webapp-tester` 等——与 harness `roles/`（coder/evaluator/code-reviewer/task-designer/dreamer）**概念高度重合**。模板结构（`slug/name/description/instructions/skills`）正好是 L1 角色定义,`CreateAgentFromTemplate` 可一键建成 Agent。

### 验证用 AI-GAME 的真实角色（同步功能 = 核心交付）

用户已定调：**同步功能就是要做的核心**,参考工程 = AI-GAME。所以验证不再走"内置模板预制"的退路,直接验证同步本身（详见 §六点七验证路径）：关联 AI-GAME → 同步出 6 个真实角色 → 任务规划用上 → 端到端跑通。

内置 25 个 agent template 仍是有用的兜底（无 harness 结构的普通 repo 时可手工建角色），但本轮主验证路径是**同步 AI-GAME 的 `.claude/agents/`**。

## 七、与既有设计的衔接

- 不改 §design-task-mode 的四栏→两栏对话式（[[task-page-two-column-conversational]]）;成员入口挂在合并主窗口/挂起树旁。
- PMO 识别 = 派任务给 agent,严守"后端不调 LLM"（[[llm-decompose-via-leader-task]]）。
- 动态小队语义不变（[[task-mode]]）,只是成员来源升级。
- L1/L2 双层 + repo SSOT（[[two-layer-roles-and-repo-ssot]]）是阶段 2 的理论底座。
