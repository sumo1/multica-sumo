# 需求文档：Assistant 能力增强

> 任务 ID: 260608-assistant-enhancements
> 状态: **需求细化中**（本轮只做需求梳理 + 验证方向，不写代码、不做技术方案）
> 录入日期: 2026-06-08

本任务包含若干相关但可独立推进的需求：

- **需求一**：在 Assistant 里按 runtime（Codex / 本机 Claude Code 等）直接聊天
- **需求二**：基于角色的复杂任务派发与多角色并行执行 + 状态可视化
- **需求三**：集成 `computer-use-harness`，让角色在本机执行 UI 操作

需求一是「单 runtime 对话打通」，需求二是「多角色并行编排」，需求三是「给角色加本机操作能力」。需求一是需求二的前置基础；需求三是需求二的能力扩展。

### 两种执行模式（同一对话入口）

需求一与需求二不是两个割裂功能，而是**同一个 Assistant 对话框里的两种执行模式**，共享同一套底座（同一会话框、同一份 runtime 选择、同一条 daemon→runtime 执行链）：

```
Assistant 同一个对话入口
  ├─ 普通聊天模式   选 runtime(codex / claude-code / openclaw) → 直接对话      [需求一]
  └─ goal 模式      选 runtime + /Goal·/goal → PMO 拆解 + 多角色并行编排        [需求二]
```

区别只在「这次消息是普通对话，还是触发 goal 编排」。这意味着需求二是在需求一打通的对话之上**加一个模式开关**，不是另起炉灶。两种模式都支持选择 codex / claude-code / openclaw 等 runtime。

## 全局约束（贯穿所有需求）

- **端形态：桌面客户端优先，不依赖浏览器。** 所有新功能以 `apps/desktop`（Electron）为目标端。
  - 现状澄清：功能本身不绑浏览器——`apps/web` 与 `apps/desktop` 共用同一套 `@multica/views` 页面。「启动后还在浏览器里」是因为跑的是 `make dev` / `pnpm dev:web`（起 Next.js 到 localhost:3000）。用 `pnpm dev:desktop` 即在 Electron 窗口里跑。
  - desktop 主进程已具备本机能力：拉起本地 daemon（`daemon-manager.ts`，`execFile`）、起本地端口、IPC（`daemon:*` handlers）、`fix-path` 修复 GUI PATH。
  - **更正（design 调研后）**：本地 daemon **不是 API server**——它只暴露 `/health`、`/shutdown`、`/repo/checkout`，业务 API（chat/issue/agent）只在中心 server（:8080）。所以「单向连本地」不是改 config 那么简单，本地没有业务 API 在听。
  - **已定方案：路径 C** —— desktop 拉起一个**本地完整 server 实例**（`multica server --local-mode`），渲染进程 `apiUrl` 指向它。改动约 100-200 行，职责干净（daemon 仍是纯 executor）。详见 `design.md`。

---

## 探查到的现状基线（写需求前的事实校准）

写需求前先把代码现状摸清，避免把「已有功能」当成「新需求」。以下为 2026-06-08 探查结论：

### 已存在、可直接复用

| 能力 | 位置 | 说明 |
|------|------|------|
| Assistant 页面（左列会话 + 右侧聊天） | `packages/views/assistant/` | 双栏布局已实现 |
| 新建会话对话框（选 agent + 选 runtime） | `new-session-dialog.tsx` + `RuntimePicker` | UI 已实现 |
| Runtime 列表 | `GET /api/runtimes`，`provider` 枚举 = `claude` / `codex` / `openclaw` / `gemini` / … | 含本机 local 与 cloud |
| **本机进程连通性检测** | daemon heartbeat → `status: online/offline` + `last_seen_at`；前端 `useRuntimeHealth` 推导四态（online / recently_lost / offline / about_to_gc） | 用户所说「连通性是已有功能」属实 |
| 发消息 / 消息流 / 停止任务 / 实时刷新 | `sendChatMessage` + WS 事件 `chat:message` / `chat:done` / `task:message` | 复用 chat 组件 |
| 会话恢复（接着上次 session 聊） | `chat_session.session_id` + `work_dir` | 已实现 |
| 任务队列 + claim/complete + 任务状态事件 | `agent_task_queue`，WS `task:queued/running/progress/completed/failed` | 多任务并行执行的底层运输层 |

### 已确认的缺口

| 缺口 | 证据 | 归属需求 |
|------|------|---------|
| **runtime 选择被丢弃**：对话框选了 `runtimeId`，但 `handleCreateSession` 调 `createChatSession` 时只传 `agent_id` + `title`，`runtime_id` 没传出去 | `assistant-page.tsx:128-131`；后端 `chat_session` 表本身有 `runtime_id` 字段 | 需求一 |
| 会话头部不显示「当前会话绑定的是哪个 runtime」 | 现有 UI 无此元素 | 需求一 |
| 「角色」概念、复杂任务拆解派发入口、多子任务并行编排面板 | 不存在 | 需求二 |

> 注：`provider` 枚举里没有字面量 `"claude-code"`，本机 Claude Code 对应 `provider: "claude"` + `runtime_mode: "local"`。用户口语「本机 Claude Code」= 这类 runtime。

---

## 需求一：在 Assistant 里按 runtime 直接聊天

### 目标（一句话）

在现有 Assistant 的同一个聊天对话框里，让用户明确选择「这次对话用哪个 runtime」（Codex、本机 Claude Code 等），并且选择真正生效。

### 真问题

不是「没有聊天功能」——聊天、选 runtime 的 UI 都有。真问题是**用户在对话框里选的 runtime 当前不生效**（前端没把 `runtime_id` 传给后端），导致「选 Codex 还是本机 Claude Code」这个动作是空的。这是一条断掉的线，不是一个缺失的功能。

### 范围（已确认）

- [ ] **接通 runtime_id**：创建会话时把用户选的 `runtime_id` 真正传到后端并落库到 `chat_session.runtime_id`。
- [ ] **会话上下文显示 runtime**：进入一个会话后，能看到「当前对话绑定的 runtime + 其在线状态」。
- [ ] **连通性反馈**：选中/对话中的 runtime 离线时，给出明确提示（复用已有 `useRuntimeHealth`，不重造检测）。
- [x] **runtime 创建时绑定、不可变**（已确认）：同一会话不支持中途切换 runtime；要换 runtime 就新建会话。
- [x] **agent 与 runtime 关系保持现状**（已确认）：维持「先选 agent 再选 runtime」的顺序与现有默认值规则，本任务不改。
- [x] **选离线 runtime → 阻止创建**（已确认）：runtime 创建时绑定且不可变，若允许绑定离线 runtime 会造出永远跑不了的死会话。离线 runtime 在新建会话流程中不可选 / 选中后阻止创建并提示。

### 明确不做（除非你提出）

- 不重做聊天交互范式（不改成 ChatGPT/Cursor 那种顶部切模型）——你已确认本轮先梳理需求，交互范式问题留到方案阶段再议。
- 不新增 provider 类型、不碰 daemon 拉起进程的逻辑。

### 验证方向（这条需求怎么算做到了）

> 需求阶段先写「验证方向」，方案阶段再细化成可机器判定的验收契约。

1. **runtime 生效可验**：创建会话选定某 runtime 后，查 `chat_session.runtime_id` = 所选值；发一条消息，该任务被分配到所选 runtime 执行（不是 agent 默认 runtime）。
2. **选择可见可验**：会话视图能读出当前绑定 runtime 的名称与在线状态。
3. **绑定不可变可验**：已创建会话无「切换 runtime」入口；`chat_session.runtime_id` 创建后不被更新。
4. **离线提示可验**：把目标 runtime 置为 offline，新建会话流程出现明确的不可用/阻止提示，且不白屏、不静默失败。
5. **负面用例**：尝试用离线 runtime 创建会话 → 按最终拍板的策略（阻止 / 允许并标记）给出确定行为。

### 待澄清点

1. ~~同一会话能否中途切换 runtime？~~ → **已定：创建时绑定、不可变。**
2. 选离线 runtime 时：阻止创建 / 允许但标记不可用？ → **倾向阻止创建，待你最终确认。**
3. ~~agent 与 runtime 的选择顺序与默认值？~~ → **已定：保持现状。**

---

## 需求二：基于角色的复杂任务派发与并行执行（本轮只出方案设计）

> 你已确认：**角色复用现有 Agent**；**需求二本轮只到方案设计，不写代码**。
> 所以这一节是「需求轮廓 + 方案要回答的问题」，方案细节进下一轮的 `design.md`。

### 目标（一句话）

借用 dev-roleplay-harness 的思路：用户给系统派发一个复杂任务（goal），由一个 **PMO 任务管理者**统筹，先派给一个「规划角色」agent 自动拆解成带角色的子任务，再并行/串行执行，并在**对话右侧的任务状态栏**实时呈现进度与可操作项。

### 真问题

复杂任务现在只能整体丢给单个 agent，缺四样东西：
1. **拆解**：把一个复杂任务自动拆成多个带角色的子任务（缺规划入口）。
2. **统筹**：一个能执行、监控、协调、处理整个 goal 生命周期的「管理者」（缺编排层）。
3. **并行执行**：多个子任务并行/串行下发（底层任务队列已有，缺编排）。
4. **可观测**：对话旁实时看到每个子任务跑到哪了（缺状态视图）。

底层运输层（任务队列、claim/complete、`task:*` 状态事件）已有，缺的是上层的「规划 → 统筹 → 编排 → 可视化」。

### 三层角色模型（已定调）

> 角色一律**复用现有 Agent**（= 配置好 instructions 的 Agent 实体），不新建独立的「角色模板」数据模型。

```
┌─ PMO 任务管理者（最外层统筹）───────────────────────────┐
│  · 接收用户的复杂任务（goal）                            │
│  · 运行在 goal 模式（Codex 或 Claude Code 支持）          │
│  · 职责：执行 / 监控 / 协调 / 处理                        │
│                                                          │
│  ├─ 规划角色 agent（自动拆解）                            │
│  │   把 goal 拆成带角色、带依赖关系的子任务               │
│  │                                                       │
│  └─ 执行角色 agents（并行/串行承担子任务）                │
│      每个子任务分配给一个角色 agent + runtime 去跑        │
└──────────────────────────────────────────────────────────┘
```

- **PMO 管理者**：最外层，统筹整个 goal。支持以 **goal 模式**（Codex / Claude Code 的 goal 能力）执行、监控、协调、处理。并行度、失败处理等编排决策**由它来定**，不写死在系统里。
- **规划角色**：PMO 派发的第一个 agent，负责把复杂任务自动拆解。
- **执行角色**：承担拆解出的各子任务，可并行可串行。

### 状态视图（已定调：借鉴 Codex 右侧 Progress 面板）

参考 Codex 实现（见用户提供截图）：**对话右侧常驻一个任务状态栏**，分区呈现：

- **任务 / 改动区**（类比截图 Environment）：当前 goal 概览、分支/改动量、可执行的常见操作（如 commit/push、review、undo）。
- **进度区**（类比截图 Progress）：子任务列表，每条带状态圆圈（✓ 完成 / ◌ 进行中 / ○ 待办 / ✗ 失败），实时流转。
- **来源区**（类比截图 Sources）：相关引用 / 产物链接。

落点：**嵌在 Assistant 对话视图右侧**（不是独立页面），与左列会话、中间对话框形成三栏。

### 双层角色模型（已定调）

角色定义拆成两层，**两层各有各的家，组合发生在派发时**。这样既复用现有 Agent（不新建角色模板实体），又把工程维度的差异独立出来，避免「同一角色服务多 repo 就抄多份 instructions」的重复。

```
Layer 1 — 角色职责（通用，与 repo 无关）
  · "我是 coder / evaluator"、Soul、职责、边界、工具白名单
  · 跨工程通用
  · → 复用现有 Agent 实体（workspace 级），不新建模板模型

Layer 2 — 工程规范（工程维度，绑定具体工程）
  · "这个工程用 snake_case"、该 repo 的约定、历史沉淀
  · 绑定到具体工程
  · → 不塞进 Agent；存在工程对应的实体/仓库里（见下「沉淀落点」）

派发时组合：PMO 派任务给角色 = 角色职责(L1) + 该工程规范(L2) 拼成本次实际上下文
```

为什么要拆：若把职责和规范揉进同一个 `Agent.instructions`，同一个 coder 角色服务 3 个 repo 就要建 3 个 agent，各抄一遍通用 Soul，只差规范几行——改一次通用职责要改 3 处。拆开后：通用职责一处定义（SSOT）、工程规范一处定义（SSOT）、组合在派发时发生。这与 harness 自身结构同构：`roles/`（与语言/框架无关）= L1，`docs/engineering/`（工程规范）= L2。

### 沉淀落点与「已有 harness 工程导入」（核心架构决策）

**SSOT 归属：repo 是 SSOT，平台是它的投影（projection），不是反过来。**

平台不抢 SSOT 身份——它从工程对应的 git repo 读取、同步、呈现角色定义与工程规范。这天然杜绝了 harness doctrine 最反对的腐坏（平台改了规范但 repo 没同步）。

**关键能力——导入已有 harness 工程**：一个工程若已经用了 dev-roleplay-harness（repo 里已有 `roles/`、`docs/engineering/`、`docs/knowledge/`、`docs/task/`），接入 multica 时平台**读取约定目录，把角色职责（L1）与工程规范/沉淀（L2）同步到平台**，project 维度信息瞬间补全，且真相仍留在仓库中。映射关系：

```
repo 里的目录                              →  平台投影
─────────────────────────────────────────────────────────
roles/*.md 或 .claude/agents/*.md          →  角色定义（L1，可复用为 Agent 配置）
docs/engineering/                          →  project 工程规范（L2）
docs/knowledge/                            →  project 跨任务知识（L2）
docs/task/{id}/                            →  project 任务沉淀
```

multica 现状约束：Project 可挂 `ProjectResource`（github_repo / local_directory），也可不挂。要用 roleplay 派发的 project，倾向要求先绑一个 repo（L2 才有家）。

### 这套模型 design 阶段必须想死的点（先标记，不在本轮解决）

1. **同步方向**〔倾向 + 待拍板〕：强烈倾向**单向 `repo → 平台`**。允许平台回写 repo 会再造两个 SSOT 漂移。平台内编辑要么禁止，要么走「生成 diff → 人提交回 repo」，不让平台成为新真相。**这是架构承重墙，待你最终拍板。**
2. **新鲜度**：repo 的 `docs/engineering/` 改了，平台投影如何刷新？初次绑定导入一次 + 后续（手动重新同步 / git webhook / 派发时实时读）？呼应 doc-refresher「起点必须新鲜」。
3. **格式契约**：平台靠什么解析 md？认目录约定（`roles/*.md` = 角色），还是认 frontmatter（`.claude/agents/` 带 `name`/`description`）？约定越硬导入越稳。
4. **无 harness 结构的 repo**：接入普通 repo（没有 `roles/`、`docs/engineering/`）怎么办——留空，还是提供「一键初始化 harness 骨架」？

### 方案设计需要回答的问题（进 design.md 时逐条解决）

1. **PMO 与 goal 模式**：Codex / Claude Code 的 goal 模式具体是什么能力、当前 daemon/runtime 是否已支持？PMO 管理者是一个特殊 agent，还是一个新的编排实体？
2. **拆解入口**：规划角色 agent 如何被触发、拆解结果的结构（子任务 + 角色 + 依赖）存哪——复用 `agent_task_queue`，还是需要新的「goal / 子任务组」概念？
3. **依赖与并行**：子任务依赖关系怎么表达？并行判定（呼应 README「文件范围互斥 + 约束显式 + 验收独立」）由 PMO 决定还是规划角色给出？
4. **编排与下发**：并行组如何同时下发到多个 runtime？谁维护 goal 的状态机？
5. **状态视图数据源**：右侧面板的数据从哪来（WS `task:*` 已有，但 goal/子任务组聚合层是否需要新增）、展示粒度、可操作项有哪些。
6. **与 harness 协议的取舍**：双契约 / evaluator 复跑 / 三层记忆，这次哪些采纳、哪些先不做。
7. **破坏性评估**：是否改动 `agent_task_queue` 结构、是否影响现有单任务执行链路。

### 验证方向（方案阶段产出，先列骨架）

> 本轮不写代码，这里只列「方案要能支撑的验证目标」，作为 design.md 的验收方向。

1. **可统筹**：用户给一个复杂 goal，PMO 管理者能接管整个生命周期（拆解 → 调度 → 监控）。
2. **可拆解**：规划角色能把 goal 产出 ≥2 个带角色、带依赖关系的子任务。
3. **可并行**：被标为并行的子任务，确实能同时下发到不同 runtime 并行执行（不串行排队）。
4. **可观测**：右侧状态栏实时反映每个子任务的状态流转（待办 → 进行中 → 完成/失败）。
5. **不破坏**：现有单 agent 聊天 / 单任务执行链路行为不变。

### 待澄清点

1. ~~拆解由谁做？~~ → **已定：先派给规划角色 agent 自动拆。**
2. ~~状态看板落点？~~ → **已定：嵌在对话右侧（借鉴 Codex Progress 面板）。**
3. ~~并行度上限、失败处理？~~ → **已定：由 PMO 任务管理者决定，不在系统层写死。**
4. **goal 模式的触发方式**（已确认）：Codex 与 Claude Code 都支持 goal 模式，触发方式有两种——
   - **命令触发**：Codex 用 `/Goal`，Claude Code 用 `/goal`（注意大小写不同）。
   - **自然语言触发**：形如「通过目标模式，完成 xxxx」。

   这解开了需求二最大的可行性疑虑：goal 模式是底层 CLI 真实存在的能力，不需要从零造编排引擎。PMO 管理者本质上就是「把用户的复杂 goal 翻译成对应 runtime 的 goal 触发指令并交给它统筹」。

   〔仍待 design 阶段实地调研〕**daemon 侧集成链路**：上面是「客户端/用户如何触发 goal 模式」，但 **daemon 当前如何把一条 goal 指令喂给它拉起的 codex / claude-code 进程**、goal 模式下子任务进度如何回流成 `task:*` 事件——这条链路是否已通、需要补什么，仍要进 design 阶段读 `server/internal/daemon/` 的实际代码确认。触发方式已知 ≠ 本仓集成已通。

---

## 需求三：集成 computer-use-harness（让角色操作本机）

> 本轮只到需求梳理 + 方案要回答的问题，不写代码。

### 目标（一句话）

把上层目录的 `computer-use-harness`（一套 CLI-first 的本机 UI 操作执行底座）集成进 multica，让角色 agent 在执行 goal 时能安全地观察和操作本机应用，工具调用在本工程内统一封装。

### 真问题

角色现在只能在「代码 / 终端」维度干活，碰不到本机 GUI。要让 agent 真正完成「打开某 app、点按钮、填表单」这类桌面任务，需要一个**可解释、可拦截、可追踪、可复现**的本机操作底座——而这正是 `computer-use-harness` 已经做好的事，不该重造。

### computer-use-harness 现状（探查结论，2026-06-08）

- **定位**：macOS 优先的本机 computer-use runtime，CLI-first，机器可读 JSON 输出。本身也是一个 dev-roleplay-harness 工程（有 `docs/task`、`docs/engineering`）——可作为「导入已有 harness 工程」的首个真实用例。
- **六个一等公民协议**（`src/core/contracts.ts`）：Target / Observation / Action / Policy / ActionResult / Trace。动作有 policy 拦截、JSONL trace 落盘（`.computer-use/traces/`）。
- **工具组合（能力链）**：8-10 个通用 Capability 按优先级自动降级——WaitForState → NavigationVerifier → DialogHandler → AX 抽取 → ScreenshotVision → TextInput → AXElementFinder → 坐标点击。App Adapter 只提供 semantic hints，不实现逻辑。
- **执行底座**：Swift native helper（`native/mac-helper/`），通过 **stdio JSON-RPC 2.0 长连接**通信，支持 permissionStatus / listApps / listWindows / click / type / key / scroll 等。
- **集成形态**：首选 **CLI 子进程 + JSON 输出**（最简单稳定，适合 Go/Electron），也可 TS API 直调 / RPC server。

### 集成版图（与需求二咬合）

```
multica desktop (Electron, 本机能力齐备, 单向连本地 daemon)
   └─ daemon (Go, 已能 spawn 子进程)
        ├─ 拉起 codex / claude-code (goal 模式)          ← 需求二
        └─ 调用 computer-use CLI (本机 UI 操作)           ← 需求三
             └─ Target/Action/Policy/Trace + Swift helper
```

统一封装点在 **daemon**：把 computer-use 注册成 daemon 可调用的一类工具/能力，让角色 agent 在 goal 执行中能调它操作本机。

### 方案设计需要回答的问题（进 design.md 时逐条解决）

1. **集成形态选型**：daemon 以 CLI 子进程调用 computer-use（每次起进程），还是走它的 stdio JSON-RPC 长连接（常驻）？前者简单、后者低延迟。
2. **工具暴露**：computer-use 的能力以什么粒度暴露给角色 agent——一个「操作本机」的高层工具，还是把 Capability 拆成多个细工具？与 codex/claude-code 自身的工具系统怎么并存？
3. **policy / 权限**：computer-use 自带 policy guard + macOS 权限（Accessibility/Screen Recording）。multica 这层要不要再加一层授权闸口？trace 要不要回流到平台的任务状态视图？
4. **trace 与可观测**：computer-use 的 JSONL trace 能否接入需求二的右侧状态栏（作为「来源/产物」）？
5. **打包与分发**：computer-use 是独立 repo（含 Swift helper 编译产物）。集成是 vendoring、子模块、还是 daemon 运行时按路径调用？跨机器分发怎么办。
6. **平台边界**：computer-use 是 macOS 优先。非 mac 端如何降级（功能不可用提示，不白屏）。

### 验证方向（方案阶段产出，先列骨架）

1. **可调用**：daemon 能调起 computer-use 完成一个最小本机操作（如 open app + click），拿到结构化 ActionResult。
2. **可拦截**：policy 阻止的动作不被执行，且有明确反馈。
3. **可追踪**：操作产生 JSONL trace，且能在平台侧看到（至少有入口）。
4. **可降级**：非 macOS 端明确提示不可用，不崩。
5. **不破坏**：现有 daemon 执行链路（codex/claude-code 任务）不受影响。

### 待澄清点

1. 集成形态：CLI 子进程 vs 常驻 JSON-RPC——倾向先 CLI 子进程（贴合 daemon 现有 spawn 模式），待 design 确认。
2. computer-use 作为独立 repo 的引入方式（子模块 / vendoring / 按路径调用）——你有偏好吗？
3. 这次要不要做 trace 回流到状态视图，还是先打通调用、trace 暂留本地？

---

## 本轮交付物清单（需求梳理轮）

- [x] `requirement.md`（本文件）：两个需求的目标、真问题、现状基线、范围边界、验证方向、待澄清点
- [x] 需求一待澄清点：已基本确认（仅剩「离线 runtime 阻止/允许」待最终拍板）
- [x] 需求二待澄清点：已确认（规划角色自动拆 + 右侧状态栏 + PMO 分层 goal 模式）

## 决策记录（本轮确认）

**需求一：**
- runtime 创建时绑定、不可变。
- agent/runtime 选择顺序与默认值保持现状。
- 离线 runtime → 阻止创建（已确认）。

**需求二：**
- 角色复用现有 Agent，三层模型：PMO 管理者 → 规划角色 → 执行角色。
- PMO 支持 Codex / Claude Code 的 goal 模式，统筹执行/监控/协调/处理。
- 拆解由规划角色 agent 自动完成。
- 状态视图嵌在对话右侧，借鉴 Codex Progress 面板（任务/改动 + 进度 + 来源三区）。
- 并行度、失败处理由 PMO 决定，不写死在系统层。
- goal 模式触发已确认：Codex `/Goal`、Claude Code `/goal`，或自然语言「通过目标模式，完成 xxxx」。daemon 侧集成链路待 design 阶段实地调研。
- **双层角色模型**：L1 通用职责（复用现有 Agent，workspace 级）+ L2 工程规范（工程维度），派发时组合。
- **SSOT 归属：repo 是 SSOT，平台是投影**。平台从 repo 读取/同步，不抢真相。
- **导入已有 harness 工程**：接入带 harness 结构的 repo 时，读 `roles/`、`docs/engineering/`、`docs/knowledge/`、`docs/task/` 同步到平台，project 维度信息瞬间补全。
- **同步方向：单向 repo → 平台（已确认）**。平台只读/投影，不回写 repo。
- **端形态：桌面客户端优先，不依赖浏览器（已确认）**。目标端 `apps/desktop`，收敛到单向连本地 daemon 端口。
- **集成 computer-use-harness（需求三）**：让角色操作本机，统一封装在 daemon。倾向先以 CLI 子进程形态调用。
- computer-use-harness 自身也是 harness 工程，是「导入已有 harness 工程」的首个真实用例。
- **端形态：路径 C（已确认）**——desktop 拉起本地完整 server 实例（`multica server --local-mode`），daemon 仍是纯 executor。约 100-200 行。
- design.md 已产出：需求一到可执行级（含双契约），需求二/三/端形态到方案级。

### 收尾确认（2026-06-08 全部敲定）

- 需求一离线 runtime：**仅前端拦截**（后端 online 校验列为后续加固项；权衡见 design 1.4）。
- 端形态本地 server：**仍连中心 DB**，与 daemon 并存（先解决「不依赖浏览器」，不追求真离线）。
- 需求二 PMO 实体：**扩展 autopilot_run**（非新建表），DAG 先存 plan JSONB。
- 需求二失败处理：**继续其余 + 标记失败**，仅阻塞下游依赖，PMO 汇总交用户决定。
- 需求三接入：**用户手动装 computer-use CLI + SKILL.md 作 workspace skill 手挂**（multica 侧近零改动）。
- 施工顺序：**一 → 二 → 三**，端形态可穿插。

## 下一步（等你确认）

1. 审阅本文档，确认现状基线、真问题、以及本轮回填的决策是否准确。
2. 拍板需求一最后一个点：离线 runtime 阻止创建 / 允许但标记。
3. 确认后进入下一轮：
   - **需求一**：走 `breakdown.md`（子任务拆解）+ `design.md`（技术方案 + 双契约 plan）。
   - **需求二**：只出 `design.md`。**第一步是调研 goal 模式现状**（Codex / Claude Code 在本仓 daemon 侧的支持程度），这是方案可行性的前提。

---

## 实现状态（2026-06-09 收口）

> 本文档正文保留 06-08 需求细化原貌（含决策演进痕迹）。06-09 实现期有两处早期决策被翻转，**以下为准**，详见 `design.md` 2.8–2.11 与 `memory/2026-06-09-*`。

**需求一（runtime 直聊）**：已实现并合并到 main（runtime_id 全链打通、离线前端拦截）。

**需求二（PMO 多角色编排）**：已实现，端到端可用（DB → 后端 → UI）。两处旧决策翻转：

- ~~扩展 autopilot_run~~ → **新建 goal_run / goal_subtask 独立表**（autopilot_run 的 NOT NULL/CHECK/1:1 约束不可破）。见 `memory/2026-06-09-goal-tables-not-autopilot.md`。
- ~~DAG 先存 plan JSONB~~ → **goal_subtask 行 + depends_on UUID[] 显式表达 DAG**（可单节点查询/更新）。

已落地能力：
- goal 生命周期 + DAG 调度（完成解锁下游 / 失败阻塞 / 自动重试 / partial 上卷）。
- **LLM 自动拆解**：派规划任务给 squad leader（后端不直接调 LLM）。见 `memory/2026-06-09-llm-decompose-via-leader-task.md`。
- **动态工作流**：squad = 成员池（非固定团队），PMO 基于目标动态设计；对抗验证节点（kind=verify，pass 放行 / reject 有界重跑）。见 `memory/2026-06-09-dynamic-workflows-verify-nodes.md`。
- 助理页 goal 模式开关 + GoalStatusTree 实时状态树。

**需求三（computer-use skill）**：已落地。multica 侧零代码改动——产出是 `computer-use-harness/SKILL.md`（教 agent 调本机 CLI，已对真实接口逐条验证）。关键校正：computer-use CLI 是用例驱动，非自由原子命令。见 design 3.5 + `memory/2026-06-09-computer-use-skill.md`。

**已追加完成（2026-06-09）**：
- 实机验证通过（真 LLM 上 PMO 自主设计带对抗验证的工作流，完整跑通）。
- 失败处理全套：自动重试 → 升级裁决（重试/改派/编辑 spec/跳过）→ 人工接管（绑失败节点 agent 的 takeover 聊天会话）。见 design 2.13/2.14 + `memory/2026-06-09-failure-intervention.md`、`2026-06-09-human-takeover.md`。
- 需求三 computer-use skill（SKILL.md，multica 零改动）。

**尚未做（下一阶段候选）**：需求文档写入目标 repo（双 SSOT 沉淀）、端形态路径 C（desktop 拉本地 server）、给 computer-use CLI 加原子动作命令（属 computer-use-harness 自身增强）。

## 重构：任务模式（2026-06-10）

实机 dogfood 后用户反馈三个偏差，重构为**任务模式**（取代"助理页目标开关 + 固定 squad"）：
- 目标模式独立成顶级「任务」页（与助理分开）。
- PMO = workspace 默认规划层（非预定义成员），成员一个个挑组合，确认时**动态建「XXX 目标小队」**。
- 四栏布局：任务列表 / 讨论+成员+状态树 / 主子会话输出流。
- 三阶段：讨论（和 PMO 多轮）→ 确认 → 执行。多轮讨论 UI 已落地（讨论 chat 绑 PMO）。

后端执行引擎（DAG/对抗验证/干预/接管）零改动复用。实机验证 PMO 自主拆出 3 节点带对抗验证工作流并完成。详见 `design-task-mode.md` + `memory/2026-06-10-task-mode.md`。
