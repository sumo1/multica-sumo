# 下一次起始文档：任务模式 UI 优化

> 目标：打磨「任务」页（Task Mode）的 UI/UX。功能已端到端跑通（规划 → 执行 → 对抗验证 → PMO 汇总收口），这一轮**不碰执行引擎**，只优化前端呈现与交互。

## 进度（2026-06-10 第一轮已完成）

**4 栏 → 2 栏对话式重构已落地**，详见记忆 [`task-page-two-column-conversational`](memory/2026-06-10-task-page-two-column-conversational.md)。已处理：

- ③④ 合并为单一对话主窗口（讨论线常驻 + 钉底输入框；planning/summary 经 `ChatMessageList.footerSlot` 追加，**完成后仍可继续会话**）。
- 对话即创建（删掉创建表单/弹窗，点 `+` 直接空建 discussion 任务）。
- 状态树进右上角 `Popover`（收起态 N/N 进度徽章）+ 主任务入口（取代弱面包屑）。
- 顺带修：列表 status raw-enum 直渲（→ `task_page.run_status.*` i18n + enum-drift 降级）、goal-status-tree 硬编码色（→ `--success`/`--warning`/`--primary` 语义令牌）、`+` 按钮 a11y label。

**下面 §2 的粗糙点里，已覆盖**：四栏边界/留白（合并后简化）、状态树视觉层级（语义令牌 + 徽章）、④ 收口视觉权重（summary 段升级为 primary 强调卡片）、子会话切换面包屑、任务列表②信息密度（卡片化）。**仍待打磨**：空/中间态文案细化（discussion/失败/部分完成各态）、响应式窄窗退让、子/主切换过渡动效。⚠️ 末环"发消息→PMO 回复"未经 computer-use 真机验证（Tiptap 编辑器打字限制），建议手动确认一次。

## 0. 先读这些（恢复上下文）

1. **本文件** —— 你现在在读的就是起点。
2. `design-task-mode.md` §三（四栏布局）、§七点六（④ 输出流落地）、§七点七（PMO 收口）—— 当前交互模型与最近四连修。
3. `memory/README.md` —— 记忆索引，重点看：
   - `2026-06-10-task-mode.md`（独立任务页 + PMO + 动态小队）
   - `2026-06-10-execution-output-visibility.md`（④ 栏渲染：复用 TimelineView、h-full 坑、WS 广播坑）
   - `2026-06-10-pmo-summary-closeout.md`（PMO 收口/汇总）
4. 项目 `CLAUDE.md` → "Task Mode (Goal Orchestration)" 一节的**铁律**（后端不调 LLM、FK-less 任务必须走 ResolveTaskWorkspaceID、④ 复用 TimelineView 不要造平行渲染器）。

## 1. 关键文件

| 区域 | 文件 |
|------|------|
| 任务页（四栏主体） | `packages/views/tasks/components/tasks-page.tsx` |
| ④ 输出流壳 | `packages/views/tasks/components/task-stream.tsx`（数据壳，渲染交给 TimelineView）|
| 共享时间线渲染器 | `packages/views/common/task-transcript/timeline-view.tsx`（chat + 任务页共用）|
| ③下 状态树 | `packages/views/assistant/components/goal-status-tree.tsx` |
| markdown | `packages/views/common/markdown.tsx` |
| i18n | `packages/views/locales/{en,zh-Hans,ja,ko}/chat.json` → `task_page.*` |
| 设计令牌/组件 | `packages/ui/`（shadcn，Base UI primitives，base-nova 风格）|

## 2. 已知 UI 粗糙点（来自本轮 dogfood 截图，待你确认/排序）

这些是观察到的、未系统打磨的地方。**进场后先用 computer-use 真机截图复核一遍**（功能是活的，可以直接建任务跑），再和我对齐优先级——别照单全收。

- **四栏信息密度/留白**：③讨论、③下状态树、④输出流三块的边界、padding、滚动条不够统一（参考 issue-detail / project-detail 的间距约定）。
- **状态树（③下）**：节点图标/状态色/verify 盾牌/verdict 徽章/干预按钮的视觉层级偏平；进度条"N/N 步"和节点列表的关系不够直观。
- **④ 主会话**：planning 流 + "最终交付（PMO 汇总）"分隔目前只是一条 `border-t` + 一行小标题，收口结果是全局重点，值得更强的视觉权重。
- **④ 子会话切换**：点状态树节点 → ④ 切到子任务，面包屑"返回主会话 / 子任务名"较弱；主/子切换无过渡感。
- **空/中间态**：planning 中只有"PMO 正在规划任务…"一行；discussion 阶段、执行中、失败/部分完成各态的 ④ 与 ③ 呈现可更明确。
- **任务列表（②）**：仅标题 + status 文本，缺少进度/角色/时间等可扫描信息；长标题截断与对齐。
- **响应式/窗口宽度**：四栏在窄窗口下的退让策略未定义。

## 3. 硬约束（别破坏）

- **设计令牌**：用语义令牌（`bg-background`/`text-muted-foreground`…），禁硬编码颜色（`text-red-500`）。优先 shadcn 组件，`pnpm ui:add <c>` 装到 `packages/ui/`。
- **共享而非复制**：web/desktop 同形组件进共享包；④ 流渲染**继续复用 `TimelineView`**，不要为"好看"另起炉灶。
- **包边界**：`packages/views/` 零 `next/*`、零 `react-router-dom`、零 stores；导航走 `useNavigation()`。
- **高度链**：页根 `h-full min-h-0`（不是 `h-screen`），每层 flex 列 wrapper 带 `min-h-0`，否则内层 `overflow-y-auto` 不收口（本轮踩过）。
- **不引入多余 state**：除非设计明确要求，别加 useState/context/reducer。
- **不动后端/执行引擎**：纯前端打磨。若发现需要新后端字段，先和我对齐再动。

## 4. 验证方式

- 真机驱动：computer-use harness（`~/workplace/opensource/computer-use-harness`）驱动桌面客户端 **Multica Canary**（注意 app 名是 "Multica Canary" 不是 "Electron"；mac-helper 在 `native/mac-helper/.build/debug/computer-use-mac-helper`）。截图 → 改 → 再截图对比。
- 测试：`pnpm --filter @multica/views exec vitest run tasks/ assistant/` + `pnpm typecheck`。组件行为测试跟着组件走（`packages/views/`），别塞进 app 测试。
- dev：server 跑在 `:8080`（`/tmp/multica-server-new`，`DATABASE_URL=…/multica`，JWT_SECRET 同 `.env`）；desktop renderer 走 vite HMR，多数前端改动自动热更，改了类型/导出再重启 `pnpm dev:desktop`。

## 5. 工作方式

先对齐再写码（"偏了后面全是浪费"）。进场建议：computer-use 截当前四栏 → 列出你眼里的 top 问题 + 我的观察对齐 → 定 2-3 个高价值改动的薄切片 → 逐个改+真机验证。

## 6. 安全

shell 里有真实凭证（Bedrock/Azure/OpenAI relay）——绝不写进代码/文档；提交前扫一遍 docs。daemon 若需重启，用 `env -i HOME SHELL PATH …` 干净启动（避开已知 403 env 坑，见 `memory/2026-06-09-realmachine-daemon-must-rebuild.md`）。
