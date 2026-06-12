# step-1: 需求一 — 接通 chat session 的 runtime 选择

> 所属任务: 260608-assistant-enhancements ｜ 依赖: 无 ｜ 并行组: 串行（后端先于前端）

## 施工契约（给执行 Agent）

### 范围

**后端可改：**
- `server/internal/handler/chat.go`（CreateChatSessionRequest + handler）
- `server/pkg/db/queries/chat.sql`（CreateChatSession INSERT）
- `server/internal/service/task.go`（EnqueueChatTask 读取优先级）
- sqlc 生成产物（`make sqlc` 自动）

**前端可改：**
- `packages/core/types/chat.ts`（ChatSession + 请求类型）
- `packages/core/api/client.ts`（createChatSession 签名 + parseWithFallback）
- `packages/core/api/schema.ts` 或 schemas（新增 ChatSessionSchema）
- `packages/core/chat/mutations.ts`（useCreateChatSession 透传）
- `packages/views/assistant/components/assistant-page.tsx:128-131`（真正传 runtime_id）
- `packages/views/assistant/components/new-session-dialog.tsx`（canCreate 排除离线 + 头部展示）
- `packages/views/agents/components/runtime-picker.tsx`（离线项禁用）

**不可改 / 冻结边界：**
- `agent_task_queue` 表结构 — 不动
- daemon claim 逻辑、WS 事件 — 不动
- `chat_session` 表结构 — runtime_id 字段已存在（migration 060），不加新字段

### 产出清单

1. 后端 `CreateChatSessionRequest` 增 `RuntimeID *string`（可选，nil 回退）。
2. handler 校验：传了 runtime_id 时 `parseUUIDOrBadRequest` 校验格式 + 查属同 workspace（**不做 online 校验**，见 design 1.4）。
3. `chat.sql` 的 CreateChatSession：接受 runtime_id 参数，传入用之，NULL 回退 `SELECT runtime_id FROM agent`。
4. `EnqueueChatTask`：runtime 来源改为 `COALESCE(chat_session.runtime_id, agent.runtime_id)`。
5. 前端类型 `ChatSession.runtime_id: string | null`、请求类型加 `runtime_id?`。
6. `api.createChatSession` 透传 runtime_id + 走 `parseWithFallback` + 新增 `ChatSessionSchema`。
7. `assistant-page.tsx` 创建会话时真正传 runtime_id（当前漏传根因）。
8. RuntimePicker 离线项禁用（`useRuntimeHealth`），NewSessionDialog `canCreate` 排除离线。

### 约束

- **回退兼容**：runtime_id 为 NULL 必走 agent 默认（历史会话行为不变）。
- **schema 防御**：chat session 响应走 parseWithFallback（CLAUDE.md 要求，当前缺）。
- **UUID 解析**：用户输入的 runtime_id 走 `parseUUIDOrBadRequest`，不裸 `parseUUID`。
- 仅前端拦截离线，后端不做 online 校验。

### 复用参考

- 其他已走 parseWithFallback 的 endpoint（如 issues/user）——对齐 schema 写法。
- RuntimePicker 现有 `isRuntimeUsableForUser` 禁用逻辑——在其上叠加离线判断。

## 验收契约（给验收）

### 代码结构验证
- [x] `CreateChatSessionRequest` 含 `RuntimeID *string`
- [x] `EnqueueChatTask` 读取 `COALESCE(chat_session.runtime_id, agent.runtime_id)`（代码层 if-fallback）
- [x] `ChatSession` TS 类型含 `runtime_id`
- [x] `ChatSessionSchema` 存在且 createChatSession 走 parseWithFallback
- [x] `assistant-page.tsx` createSession 调用传了 runtime_id

### 命令验收
| 命令 | 通过标准 | 结果 |
|------|---------|------|
| `pnpm typecheck` | 0 error | ✅ 6/6 包通过 |
| `cd server && go build ./...` | 通过 | ✅ |
| `cd server && go test ./internal/handler/ ./internal/service/` | 通过 | ✅ |
| `sqlc generate` | 生成无 drift | ✅ |
| TS 单测（core+views） | 通过 | ✅ core 450 / views 1073 |

### 数据 / 行为验收（2026-06-08 真实 API + DB 端到端验证，全通过）
- [x] **A** 创建会话传 runtime_id → `chat_session.runtime_id` = 所选值（响应 + DB 双确认）
- [x] **B** 该会话发消息 → `agent_task_queue.runtime_id` = 会话 runtime（online claude），**非** agent 默认（offline codex）。这是需求一核心：证明会话选择真正生效
- [x] **C** 不传 runtime_id → 回退 agent 默认 runtime（兼容历史会话）
- [x] **D** 传非法 UUID → 400
- [x] **E** 传不属于本 workspace 的 runtime → 400
- 验证脚本：`~/tmpsh/e2e-verify.sh`（API localhost:8080 绕代理 + psql multica 库）

### 负面用例
- [x] 传非法 UUID 的 runtime_id → 400（parseUUIDOrBadRequest）
- [x] 喂 malformed chat session 响应 → 走 fallback（新增 schemas.test.ts 6 个用例）

### 前端验收
- [x] RuntimePicker 离线 runtime 项 disabled + 提示（blockOffline prop，仅 chat 用）
- [x] 离线选中时 canCreate=false（new-session-dialog）

### 顺带修复的预存问题（非本任务引入，阻塞验证）
- [x] `auth-initializer.tsx` dev-mock User 缺 6 个字段 → 补全
- [x] `consistency.test.ts` 缺 assistant 路由 → 补 2 处
- [x] `app-sidebar.test.tsx` paths mock 缺 assistant → 补 2 处
- [x] `session-list*.tsx`、`assistant-page.tsx` 未使用 import/变量 → 清理
- [x] ja/ko `layout.json` 缺 `nav.assistant` → 补译
