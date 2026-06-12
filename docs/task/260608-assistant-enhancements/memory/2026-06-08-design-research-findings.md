# Design 阶段调研结论（四路）

> 2026-06-08 四路只读调研，全部带 path:line 证据。下面是改变方案走向的关键事实。
>
> 更新（06-09）：本文「需求二」段提到「扩展 autopilot_run 或新建编排表」两条路——最终**选了新建表**（goal_run/goal_subtask），见 `2026-06-09-goal-tables-not-autopilot.md`。其余调研事实仍成立。

## 需求一：runtime_id 接通——比想象的更近，但语义要选

**后端数据库层已完全支持**，只是前端没传 + 创建语义是「继承 agent 默认」：

- `chat_session.runtime_id` 字段已存在（migration `060_chat_session_runtime_id`，nullable，引用 agent_runtime）。
- 现状 SQL（`chat.sql:2-4`）：创建会话时 `runtime_id` 自动 `SELECT runtime_id FROM agent WHERE id = $2`——**继承 agent 默认 runtime，不接受传入 override**。
- `CreateChatSessionRequest`（`chat.go:26-29`）只有 `agent_id` + `title`，**无 runtime_id**。
- 任务分配链路**已经按 runtime_id 工作**：`EnqueueChatTask`（`task.go:626-655`）读 `agent.runtime_id` → `CreateChatTask` 写 `agent_task_queue.runtime_id` → daemon `ListQueuedClaimCandidatesByRuntime` 按 runtime_id claim。

**关键决策点**：现在 chat task 的 runtime 来自 `agent.runtime_id`，**不是 chat_session.runtime_id**。要让「会话选的 runtime」真正生效，得改 `EnqueueChatTask` 优先读 `chat_session.runtime_id`（回退 agent 默认）。这是需求一的真正核心改动，不止是「传个参数」。

**改动点清单**（约后端 4 处 + 前端 7 处）：
- 后端：`CreateChatSessionRequest` 加 `runtime_id`、handler 校验、`chat.sql` 改 INSERT 接受参数、`EnqueueChatTask` 改读取优先级、sqlc 重新生成。
- 前端：`ChatSession` 类型 + `CreateChatSessionRequest` 类型加 `runtime_id`、`api.createChatSession`、`useCreateChatSession`、`new-session-dialog` 传参（`assistant-page.tsx:128-131`）。
- 离线阻止：`runtime-picker.tsx` 现在只按权限禁用（`isRuntimeUsableForUser`），**没按 online/offline 禁用**——要加 `useRuntimeHealth` 判断。
- schema 防御：chat API **当前没走 parseWithFallback**（CLAUDE.md 要求），需补 `ChatSessionSchema`。

## 需求二：底座比预期强，编排层确实要新建

- **并行 claim：完全支持，零改造**。多 runtime 各自 claim 并行跑，SQL 行级锁保证原子性。agent 级有 `max_concurrent_tasks`（默认 1）。
- **agent_task_queue 已有 `autopilot_run_id`、`parent_task_id`、`context JSONB`、`runtime_id` 等字段**（migration 001 + 055 + 020 等）。但**没有显式 `depends_on` / `group_id` 依赖图字段**——`parent_task_id` 只表达重试链，不是 DAG。
- **autopilot_run（migration 042）是最接近 PMO 的现有机制**：有触发源（schedule/manual/webhook/api）、状态机、concurrency policy（skip/queue/replace）、squad 分配。**但 `autopilot_run.task_id` 是 1:1 单值**，不是「一个 run → 多个子任务」。要做 PMO 编排，要么扩展它支持 N:1，要么新建编排表 + 在 `context`/`trigger_payload` 存 DAG。
- **进度回流粒度足够细**：daemon 每 500ms flush，`task:message` 含 text/thinking/tool_use/tool_result/error + seq。完整 WS 事件族 `task:queued/dispatch/running/progress/completed/failed`，前端 `use-realtime-sync.ts` 已订阅。**右侧状态栏数据源现成**。
- **可复用 UI**：`ExecutionLogSection`（issue 详情页的任务面板）+ `TaskTranscript`（消息时间线）+ active/past 分桶——是右侧状态栏的现成原型。缺的只是 DAG/依赖可视化。

## 需求二：goal 模式集成现状

- daemon 拉 codex/claude-code 是 `exec.CommandContext` 子进程 + JSON-RPC over stdin（`codex.go:541/694`）。prompt 经 `BuildPrompt(task, provider)`（`prompt.go:17`）构造，是**纯字符串**。
- **注入 `/goal` 技术可行**（改 BuildPrompt 前缀），但 daemon 侧**无指令解析、无多步拆分**——daemon 只执行单 task。
- 结论：goal 模式的「拆解 + 编排」必须在 **server 侧**做（daemon 只是 executor）。daemon 改动小（prompt 前缀 + 传 goal 上下文）。

## 全局约束：desktop 单向连本地——有坑

- **daemon 不是 API server**！它只暴露 `/health`、`/shutdown`、`/repo/checkout`（`health.go:115-191`）。chat/issue/agent 等业务 API **只在中心 server（:8080）**。
- desktop 渲染进程所有业务请求打到 `runtime-config.ts` 的 `apiUrl`（默认云端 `api.multica.ai`），daemon 只管启停/版本/repo。
- 所以「desktop 单向连本地端口」**不是改个 config 那么简单**——本地根本没有业务 API 在听。真正选项：
  - 路径 C（推荐）：desktop 拉起一个**本地完整 server 实例**（`multica server --local-mode`），改动约 100-200 行。
  - 路径 A：daemon 加 `/api/*` 反向代理转发到中心 server，约 200-300 行 + 要补 auth 中间件。
- **这条要回退给用户确认**：之前 layout 里写「约 3 行 config」是错的，本地没有业务 API 面。

## 证据来源

四路 Explore 调研，2026-06-08。相关：[[2026-06-08-goal-mode-trigger]] [[2026-06-08-two-layer-roles-and-repo-ssot]] [[2026-06-08-desktop-form-and-computer-use]]
