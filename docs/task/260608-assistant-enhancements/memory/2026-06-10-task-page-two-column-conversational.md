---
name: task-page-two-column-conversational
description: Task page UI refactor — 4 columns collapsed to 2; discussion stays a single continuous thread (planning/summary appended via footerSlot), conversational create, pinnable status-tree popover
metadata:
  type: project
---

任务页（`packages/views/tasks/components/tasks-page.tsx`）从**四栏砍成两栏的对话式布局**。
取代旧的「② 列表 / ③ 讨论+状态树 / ④ 主子会话输出」模型（见 [[task-mode]]、
[[execution-output-visibility]] 描述的旧四栏）。纯前端，零后端改动。dogfood 用户
反馈驱动：讨论和主会话其实是同一条 PMO 时间线的两半，劈成两栏是人为割裂。

**新布局（2 栏）：**

- **② 任务列表**（不变位置）：列表项卡片化——标题 + 本地化 status + N/N 进度点。
  `listTasks` 返回完整 `GoalRun[]`（带 subtasks），所以 N/N 进度**前端直接算**，无需
  新后端字段。
- **合并主窗口**（原 ③+④）：单栏 = 内容区（上）+ 钉底 `ChatInput`（下）。

**核心数据理解（别再退回二选一）：** 一个任务有**一条贯穿始终的 PMO 讨论主线**
（discussion chat）；planning / summary 是 PMO 在这条线里**交付的产物**，不是替代品。
所以主窗口内容区不是 `discussion ? : planning流`（旧版的坑——planning 一开始就把讨论
整个顶掉，用户发的消息进了 discussion 会话却看不见，"对话没了"）。正解：

- 内容区**始终渲染 discussion 的 `ChatMessageList`**（子任务视图除外）。
- planning + summary 流通过 **`ChatMessageList` 新增的 `footerSlot` prop** 追加在
  messages 之后、**同一个滚动容器内**（`chat-message-list.tsx`）。这样讨论对话常驻、
  输入框常驻，**任务完成后仍能继续跟 PMO 会话**（消息进 discussion chat → 出现在对话
  区 → PMO 回复）。
- ⚠️ 高度坑：`ChatMessageList` 自己就是 `flex-1 overflow-y-auto` 滚动容器，**不能**把它
  塞进另一个滚动父级再追加 planning——会双滚动。用 footerSlot 把内容注入它自己的滚动
  容器内是唯一干净解（同 `ChatInput.topSlot` 的 slot 注入惯例）。

**对话即创建**：点列表头 `+`（或空态 CTA）→ `create.mutateAsync({})` 空建 discussion
任务直接打开，**删掉整个创建表单 + draftTitle/draftGoal/selectedMembers state**。后端
`CreateTask(title,goal,members)` 三参数**全可空**（squad 名兜底"目标小队"、讨论标题兜底
"任务讨论"、空 goal 进 discussion），早就支持空建——唯一拦路的是前端那行
`if(!draftGoal.trim())return`。成员靠 PMO 规划时自动挑 + discussion 中 `AddTaskMember`
随时补，不再有创建前的成员勾选 UI。`new-session-dialog.tsx` 是 **assistant 页专用**，
任务页从没用它，未动。

**挂起状态树**：移到主窗口右上角 `Popover`（已装，Base UI）。收起态 trigger 带 **N/N
进度徽章**（完成=success / 失败=destructive / 进行中=primary）。展开 = 复用
`GoalStatusTree`（不另起渲染器）。

**主任务入口**：`GoalStatusTree` 顶部总进度区从 `<section>` 改成可点 `<button>` +
新 prop `onSelectMain`——点它 `setActiveSubtaskId(null)` 回主会话，主会话时 `ring-primary`
高亮。取代旧的弱面包屑。`onSelectMain` 可选，assistant 页现有调用不受影响。

**两个顺带修的真问题：**

- **列表 status 曾 raw enum 直渲**（`{tk.status}` → `executing`）：违反 enum-drift 降级 +
  本地化要求。现走新 i18n key `task_page.run_status.{discussion|confirmed|planning|
  executing|completed|partial|failed|cancelled}`（4 语言齐），enum drift 落 raw 字符串
  不崩。
- **goal-status-tree 通篇硬编码色**（`text-green-600`/`text-blue-500`/`bg-amber-500`/
  `border-amber-400`）：违反设计令牌铁律。全换语义令牌——`--success`/`--warning` token
  已存在于 `packages/ui/styles/tokens.css`，配 `text-primary`/`text-destructive`。
- **`+` 按钮无 `aria-label`**（icon-only 圆按钮，AX name=undefined）：补 aria-label +
  title。既是 a11y 改进，也解决了 computer-use 点不中小图标按钮的命中问题。

**实机验证（computer-use，Multica Canary，2026-06-10）：** 全链路截图复核通过——列表卡片
（`汇总验证2 已完成 2/2` 等本地化+进度）、合并主窗口（面包屑+planning markdown+钉底输入框）、
挂起树展开（主任务卡片高亮 + 3 节点带 verify 盾牌/verdict 通过徽章）、对话即创建（点 `+`
任务 12→13，进全新空 discussion，面包屑 `PMO 编排`、无 planning 流）。⚠️ computer-use **打字
进不去 Tiptap 富文本编辑器**（合成键事件限制），所以"发消息→PMO 回复"末环只经代码+单测确认，
未自动真机验证。

**测试：** `tasks-page.test.tsx` 重写到新契约（无表单 / 点 + 空建 / status 本地化）；
`chat-message-list` footerSlot 不破坏现有 chat 测试。41 测试全绿 + typecheck 全过。

关联：[[task-mode]]、[[execution-output-visibility]]（旧四栏 + ④ 流数据通路）、
[[pmo-summary-closeout]]（summary 流，现作为 footerSlot 的一部分）。
