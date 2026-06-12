# 设计布局图（现有功能 + 本次需求）

> 任务 ID: 260608-assistant-enhancements ｜ 2026-06-08
> 标记：✅ 已有可复用 ｜ 🔧 已有但断线/需接通 ｜ 🆕 本次新增

本文件是需求梳理轮的收口产物：把 multica 现有能力与本次三个需求 + 两条全局约束，画成统一的布局图。供 design 阶段展开。

---

## 图 1 — 系统分层全景

从「人用什么」到「最终落到本机什么」，自顶向下。

```
┌──────────────────────────────────────────────────────────────────────┐
│  端（全局约束：桌面优先，不依赖浏览器）                                  │
│                                                                        │
│  ✅ apps/desktop (Electron + Vite)        ✅ apps/web (Next.js, 浏览器) │
│     主进程能力齐备：拉 daemon / 起端口          —— 同一套页面，但本次以      │
│     / IPC / spawn / fix-path                    desktop 为目标端          │
│  🆕 路径C: desktop 拉起本地完整 server 实例(multica server --local-mode)│
│     apiUrl 指向它；daemon 仍是纯 executor（本地无业务 API，故非改 config）│
│                                                                        │
│        └────────────── 共用 ──────────────┘                            │
│              ✅ packages/views（Assistant / Issues / Projects …）        │
│              ✅ packages/core（query / store / api client）              │
└───────────────────────────────┬────────────────────────────────────────┘
                                │ HTTP / WebSocket
┌───────────────────────────────▼────────────────────────────────────────┐
│  后端  ✅ server/ (Go, Chi + sqlc + gorilla/ws)                          │
│                                                                          │
│  ✅ /api/chat/*      会话/消息/pending-task                              │
│  ✅ /api/runtimes/*  runtime 列表 + 健康                                 │
│  ✅ /api/agents/*    agent CRUD（workspace 级）                          │
│  ✅ agent_task_queue 任务队列 + claim/complete                          │
│  ✅ WS events: chat:* / task:* / daemon:*                               │
│  🆕 goal / 子任务组 编排层（PMO 状态机，待 design 定是否新增实体）          │
│  🆕 repo→平台 同步入口（导入 harness 工程：roles/ docs/engineering/ …）   │
└───────────────────────────────┬────────────────────────────────────────┘
                                │ heartbeat / claim / complete (HTTP+WS)
┌───────────────────────────────▼────────────────────────────────────────┐
│  daemon  ✅ server/internal/daemon/（本机，spawn 子进程，execenv 隔离）    │
│           ★ 统一封装点：所有「本机执行」都收敛到这里                        │
│                                                                          │
│  ✅ 拉起 runtime 进程                                                     │
│     ├─ ✅ claude (claude-code)   ┐                                       │
│     ├─ ✅ codex                  ├─ 🆕 goal 模式触发：/Goal、/goal、自然语言│
│     └─ ✅ openclaw / …           ┘     〔daemon 侧喂 goal 指令链路待调研〕  │
│                                                                          │
│  🆕 调用 computer-use-harness（本机 UI 操作）                            │
│     └─ 倾向 CLI 子进程 + JSON 输出（贴合现有 spawn 模式）                  │
└───────────────────────────────┬────────────────────────────────────────┘
                                │ CLI 子进程 / stdio JSON-RPC
┌───────────────────────────────▼────────────────────────────────────────┐
│  本机执行底座  🆕 computer-use-harness（独立 repo，macOS 优先）            │
│                                                                          │
│  协议六公民: Target / Observation / Action / Policy / ActionResult/Trace │
│  能力链(自动降级): WaitForState→Navigation→Dialog→AX抽取→Vision           │
│                    →TextInput→AXFinder→坐标点击                          │
│  执行: Swift mac-helper（stdio JSON-RPC：click/type/key/scroll/listApps）│
│  安全: policy guard 拦截  ｜  JSONL trace 落盘 .computer-use/traces/      │
└──────────────────────────────────────────────────────────────────────────┘
```

---

## 图 1.5 — 两种执行模式（同一对话入口）

需求一与需求二共享同一对话框、同一份 runtime 选择、同一条执行链；区别只在模式。

```
                    ┌─────────────────────────────────────┐
                    │   Assistant 同一个对话入口            │
                    │   ✅ 会话框 + 🔧 选 runtime           │
                    │   (codex / claude-code / openclaw …) │
                    │              🆕 模式开关              │
                    └───────────┬───────────────┬──────────┘
                                │               │
              ┌─────────────────▼───┐     ┌─────▼────────────────────┐
              │ 普通聊天模式 [需求一] │     │ goal 模式 [需求二]         │
              │                     │     │                          │
              │ 选 runtime → 直接对话│     │ 选 runtime + /Goal·/goal  │
              │ 单轮/多轮问答        │     │ → PMO 拆解 + 多角色并行    │
              │ 🔧 runtime_id 接通   │     │ 🆕 编排 + 右侧状态栏       │
              └─────────────────┬───┘     └─────┬────────────────────┘
                                │               │
                                └───────┬───────┘
                                       │ 共享底座
                          ✅ daemon → runtime 执行链
                          ✅ agent_task_queue / WS task:*
```

---

## 图 2 — Assistant 功能布局（三栏视图）

需求一接通中间对话框，需求二新增右侧状态栏。右侧状态栏仅在 goal 模式下有内容。

```
┌─────────────┬──────────────────────────────┬───────────────────────────┐
│ 左栏 ✅      │  中栏（对话）                  │  右栏 🆕 任务状态栏         │
│ 会话列表     │                               │  (借鉴 Codex Progress)     │
│             │                               │                           │
│ ✅ 会话项    │  ✅ ChatMessageList            │  🆕 任务/改动区             │
│   +agent头像 │     消息气泡 / Markdown        │   · goal 概览              │
│   +实时时长  │     / 代码块 / 附件            │   · 分支·改动量            │
│   +未读点    │     / TaskStatusPill          │   · 操作: commit/push      │
│             │                               │     review / undo         │
│ ✅ [+]新建   │  ✅ ChatInput                  │                           │
│   └─🔧弹框    │     多行/上传/发送/停止         │  🆕 进度区（子任务列表）     │
│    选 agent  │                               │   ✓ 子任务A 完成           │
│    +选runtime│  ── 当前会话绑定 runtime ──     │   ◌ 子任务B 进行中         │
│    🔧 runtime│  🆕 头部显示绑定的 runtime      │   ○ 子任务C 待办           │
│      _id 没传│     + 在线状态(useRuntimeHealth)│   ✗ 子任务D 失败           │
│    🔧 离线则  │  🆕 绑定不可变(创建时定死)      │   (数据源 ✅ WS task:*)    │
│      阻止创建│                               │                           │
│             │                               │  🆕 来源区                 │
│             │                               │   · 产物/引用链接          │
│             │                               │   · computer-use trace?    │
└─────────────┴──────────────────────────────┴───────────────────────────┘
```

---

## 图 3 — 角色双层模型 + SSOT 单向流

repo 是真相，平台是投影。角色 = 通用职责(L1) + 工程规范(L2)，组合在派发时。

```
   工程 repo（SSOT · 真相在这）                 multica 平台（投影 · 只读）
 ┌──────────────────────────────┐           ┌──────────────────────────────┐
 │ roles/ 或 .claude/agents/     │           │  ✅ Agent 实体（workspace 级） │
 │   (L1 通用职责: Soul/边界)     │──┐        │     = L1 通用职责的载体        │
 │                              │  │        │                              │
 │ docs/engineering/            │  │ 🆕     │  🆕 Project 维度信息          │
 │   (L2 工程规范)               │  │单向   →│     = L2 工程规范的投影        │
 │ docs/knowledge/              │  │同步   │                              │
 │   (L2 跨任务知识)             │  │repo   │  🆕 任务沉淀（来自 docs/task） │
 │                              │  │→平台  │                              │
 │ docs/task/{id}/              │──┘        │                              │
 │   (任务沉淀)                  │           │  ❌ 平台不回写 repo            │
 └──────────────────────────────┘           └──────────────────────────────┘
                                                          │
                          派发时组合：PMO 把任务派给角色   ▼
                    ┌───────────────────────────────────────────┐
                    │  本次执行上下文 = L1 通用职责 + L2 工程规范  │
                    └───────────────────────────────────────────┘

  说明：computer-use-harness 自身就是一个 harness 工程（有 docs/task、
        docs/engineering）→ 是「导入已有 harness 工程」的首个真实用例。
```

---

## 图 4 — 一次复杂任务（goal）的执行流

需求二的运转模型，串起角色三层 + 并行 + 状态回流。

```
 用户在 desktop 提一个复杂 goal
        │
        ▼
 🆕 PMO 任务管理者（最外层统筹：执行/监控/协调/处理）
        │  以 goal 模式运行（/Goal · /goal · 自然语言）
        │
        ├─① 派给 🆕 规划角色 agent ──► 自动拆解
        │        产出：子任务 + 角色分配 + 依赖关系
        │        并行判定：文件范围互斥 + 约束显式 + 验收独立
        │
        ├─② 编排下发（PMO 决定并行度/失败处理，不写死在系统）
        │        ┌─ 并行组 ─────────────────────┐
        │        │ 执行角色A → runtime① ┐        │   ✅ 复用 agent_task_queue
        │        │ 执行角色B → runtime② ├─ 同时跑 │      + claim/complete
        │        │ 执行角色C → runtime③ ┘        │
        │        └──────────────────────────────┘
        │        串行链：D 依赖 A → A 完成后再下发 D
        │
        └─③ 状态回流 ──► 🆕 右侧状态栏实时刷新
                 数据源 ✅ WS task:queued/running/progress/completed/failed
                 (goal/子任务组聚合层是否需新增，待 design)

  执行角色干活时可调：
    · 代码/终端（codex / claude-code 自带）
    · 🆕 本机 UI 操作（computer-use：点击/输入/截图，带 policy + trace）
```

---

## 已有 / 接线 / 新增 — 一览

| 能力 | 状态 | 位置 / 说明 |
|------|------|------------|
| Assistant 三栏（左+中） | ✅ | `packages/views/assistant/` |
| 聊天消息流/输入/停止 | ✅ | 复用 chat 组件 + WS |
| runtime 列表 + 健康检测 | ✅ | `/api/runtimes` + `useRuntimeHealth` |
| 新建会话选 runtime 的 UI | ✅ | `new-session-dialog.tsx` |
| **runtime_id 传到后端** | 🔧 | `assistant-page.tsx:128-131` 漏传，需求一核心 |
| 会话头显示绑定 runtime | 🆕 | 需求一 |
| 离线 runtime 阻止创建 | 🆕 | 需求一（已定策略） |
| 任务队列 + 并行执行底座 | ✅ | `agent_task_queue` + claim/complete + WS task:* |
| PMO 编排 / goal 状态机 | 🆕 | 需求二，待 design 定是否新增实体 |
| 规划角色自动拆解 | 🆕 | 需求二 |
| 右侧任务状态栏 | 🆕 | 需求二，借鉴 Codex Progress |
| 双层角色（L1 复用 Agent） | ✅+🆕 | L1=现有 Agent；L2=工程规范投影(新增) |
| repo→平台 单向同步/导入 | 🆕 | 跨需求基础设施 |
| desktop 单向连本地端口 | 🔧 | `runtime-config.ts` 配置（约 3 行） |
| computer-use 本机操作 | 🆕 | 需求三，daemon 统一封装 |

---

## design 阶段待解（已在 requirement.md 详列）

- goal 模式 daemon 侧集成链路（读 `server/internal/daemon/`）
- PMO 是特殊 agent 还是新编排实体；goal/子任务组是否新增表
- repo→平台 同步的新鲜度机制 / 格式契约 / 无 harness repo 的处理
- computer-use 集成形态（CLI 子进程 vs 常驻 RPC）、引入方式（子模块/vendor/按路径）、非 mac 降级
- 本地 daemon 是否暴露与 server 相同的 API 面
