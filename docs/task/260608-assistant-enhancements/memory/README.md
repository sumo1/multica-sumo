# Task Memory Index — 260608-assistant-enhancements

工程维度的记忆：跟随 git、与任何工具（Claude Code / Codex / …）共享。每个文件记一条**不可从代码直接推出**的事实（架构约束、结论、踩过的坑、外部约束）。

如果你是按问题找资料，**不要先按日期扫这里**。先看 [`../../../README.md`](../../../README.md)，按“工程角色同步 / 多智能体编排 / 端到端测试 / 工程文档沉淀”等阶段进入，再追到这里的原始条目。

这个文件保留时间线索引，方便查证“这条判断什么时候、为什么产生”。

## 06-08 设计阶段（需求细化 / 方案调研）

- [design-research-findings](2026-06-08-design-research-findings.md) — 四路 design 调研结论（runtime/编排/computer-use/端形态的现状基线）
- [goal-mode-trigger](2026-06-08-goal-mode-trigger.md) — goal 模式触发方式（Codex `/Goal`、Claude Code `/goal`、自然语言）
- [two-layer-roles-and-repo-ssot](2026-06-08-two-layer-roles-and-repo-ssot.md) — 双层角色模型（L1 通用职责 + L2 工程规范）+ repo 是 SSOT、平台是投影
- [computer-use-is-mcp-plugin](2026-06-08-computer-use-is-mcp-plugin.md) — computer-use 是插件不是大脑（三次纠正后定为 CLI + skill）
- [desktop-form-and-computer-use](2026-06-08-desktop-form-and-computer-use.md) — 端形态（桌面优先、路径 C）+ computer-use 集成形态

## 06-09 实现阶段（需求二端到端）

- [goal-tables-not-autopilot](2026-06-09-goal-tables-not-autopilot.md) — **决策翻转**：新建 goal_run/goal_subtask 而非扩展 autopilot_run（约束不可破）
- [llm-decompose-via-leader-task](2026-06-09-llm-decompose-via-leader-task.md) — **核心约束**：后端不直接调 LLM，自动拆解 = 派规划任务给 squad leader + 一个反复中招的 WS 事件坑
- [dynamic-workflows-verify-nodes](2026-06-09-dynamic-workflows-verify-nodes.md) — 动态工作流：squad = 成员池、对抗验证节点（kind/verdict、有界重跑、fail-open）
- [realmachine-daemon-must-rebuild](2026-06-09-realmachine-daemon-must-rebuild.md) — goal 模式要求 daemon 二进制也重建（server 新+daemon 旧 = 静默用错 prompt）
- [realmachine-verification-passed](2026-06-09-realmachine-verification-passed.md) — **实机验证通过**：真 LLM 上跑通完整动态工作流，PMO 自主插对抗验证；+ 两个仅实机暴露的 bug + 403 环境坑
- [stale-process-gotcha](2026-06-09-stale-process-gotcha.md) — 陈旧进程坑：改了 exports/schema/prompt 后跑着的进程要重启才生效（server/daemon/vite 都中过）
- [failure-intervention](2026-06-09-failure-intervention.md) — 失败干预按钮（重试/改派/编辑 spec/跳过）；blocked 下游解锁修复；人工重试重置 attempt 预算
- [computer-use-skill](2026-06-09-computer-use-skill.md) — 需求三：CLI+skill（multica 零改动）；关键校正：computer-use CLI 是用例驱动非自由原子命令
- [human-takeover](2026-06-09-human-takeover.md) — 人工接管：失败子任务→绑该 agent 的 takeover 聊天会话（chat_session.goal_subtask_id）；只开对话不改状态

## 06-10 重构（任务模式）

- [task-mode](2026-06-10-task-mode.md) — **独立「任务」页 + PMO 规划层 + 动态小队**（取代助理页目标开关 + 固定 squad）；实机验证 PMO 拆 3 节点带对抗验证工作流；computer-use 多 tab 定位坑
- [execution-output-visibility](2026-06-10-execution-output-visibility.md) — ④ 栏接通**过程实时流 + 结果**：enrichGoalResponse 喂 task_id/planning_task_id（漏调用站点=静默丢流）；复用 chat `TimelineView`（思考/结果分区+markdown，抽到 common）；删右下角 ChatFab+ChatWindow；`h-screen→h-full min-h-0`（拉到底读不全）；**ReportTaskMessages 用共享 ResolveTaskWorkspaceID**（否则 goal 任务 WS 广播被丢）
- [pmo-summary-closeout](2026-06-10-pmo-summary-closeout.md) — **PMO 收口/汇总**（goal_summary 任务）：规划任务一次性无收口 → 子任务全终结后派 summary 任务给 PMO 写最终交付；主会话 ④ = planning 流 + summary 流；零 schema 改动；FK-less 任务必须登记进 ResolveTaskWorkspaceID
- [task-page-two-column-conversational](2026-06-10-task-page-two-column-conversational.md) — **任务页 4 栏→2 栏对话式重构**（UI polish 轮）：③④ 合并为单一对话主窗口，讨论线常驻、planning/summary 经 `ChatMessageList.footerSlot` 追加（不再二选一替换，完成后仍可继续会话）；对话即创建（删表单，点 + 空建 discussion）；状态树进右上角 Popover + N/N 徽章 + 主任务入口；顺带修 raw-enum 直渲 + goal-status-tree 硬编码色 + `+` 按钮 a11y
- [daemon-agent-env-bedrock-403](2026-06-10-daemon-agent-env-bedrock-403.md) — **实机 goal 执行 403 连环坑**：daemon 别用 `env -i`（清掉 LLM 凭证，只该 unset 代理）；daemon 过滤掉子进程的 `CLAUDE_CODE_*` → Bedrock 失效，靠 per-agent `custom_env` 重注入；同步 agent 的 `mcp_config` 要 NULL 不要 `{}`、`runtime_mode` 要跟 runtime 一致、`custom_env` 继承已有 agent；任务 queued 不认领靠重启 daemon sweep；验证到"PMO 规划成功+角色被派发"即可，别等 LLM 真写完

## 成员编排（角色同步 + 目标→工程→成员串联，06-10）

- [design-member-orchestration](../design-member-orchestration.md) — **方案 + 落地**：`goal_run.project_id`（迁移 116）；`RoleSyncService` 读 project 的 local_directory，扫 `.claude/agents/*.md`（frontmatter+解引用）/ `roles|agents/*.md`（散文）建/更 Agent（按 name 幂等，repo→平台单向）；`POST /api/projects/{id}/sync-roles`；前端「角色」入口 + 任务页成员 popover（选工程→同步→入池→加 squad）；菜单精简（隐藏 issue/项目/小队）；实机用 AI-GAME 验证 7 角色同步→PMO 规划→派发执行全链路

## repo-SSOT 任务环节（框架翻转，06-11）

- [roster-carries-role-descriptions](2026-06-11-roster-carries-role-descriptions.md) — **修"总控选角色很奇怪"**：规划 roster 原来只给角色名+UUID，总控只能按名字猜专长。现在每行带截断的 description（首段~200字），prompt 让它"读描述按专长选"。纯增量不改选择逻辑
- [no-proactive-queueing-dispatch-direct](2026-06-11-no-proactive-queueing-dispatch-direct.md) — **不主动排队**：删 ClaimTask 的 per-agent max_concurrent_tasks 准入闸，任务直接派；runtime 真扛不住时真实错误经 FailTask 冒上来打印。保留 per-issue 串行（正确性）+ daemon semaphore（机器底线）
- [planning-roster-widened-to-workspace-pool](2026-06-11-planning-roster-widened-to-workspace-pool.md) — **修"所有子任务都派给同一 agent"**：动态 squad 常只有 leader→规划 roster 只看到自己。新增 `buildPlanningRoster` 扩出全工作区可用 agent 池（dispatch 本就不要求 squad 成员）；绑工程时 `ProjectRoleNames` 标注 repo harness 角色优先。不强制分派
- [upstream-output-handoff](2026-06-11-upstream-output-handoff.md) — **修"结果传递"bug（最初需求那条线）**：execute 子任务原来零上游输入→被迫重新推导上游产出（截图实证）。现在 depends_on 的上游 result 作为 `upstream_output` 喂进下游 prompt（buildReviewTarget 泛化为 buildUpstreamOutput）。用户选 C：A 直传已覆盖正确性，B 总控汇总交接是 token 优化暂不做
- [which-model-ran-it-attribution](2026-06-11-which-model-ran-it-attribution.md) — **任务模式显示"哪个 agent/运行时/模型响应"**：归属按任务/子任务为单位（非每消息）；model 方案3=配置值→task_usage实际值覆盖；GoalSubtaskResponse+GoalRunResponse(总控) 加 agent_name/runtime_name/model，enrichGoalResponse 解析（带缓存）；前端状态树行+子任务流+总控头部渲染；零 schema 改。聊天消息那面没做
- [restart-server-correct-db-and-proxy](2026-06-11-restart-server-correct-db-and-proxy.md) — **实机重启 server 两坑**：真数据在 `multica` 库不是 `.env` 的 `local_medeo`(空)——连错库→daemon token 401；`curl localhost` 必须 `--noproxy '*'` 否则被 http_proxy 打成 502。附重启全栈正确顺序
- [desktop-is-the-target-end](2026-06-11-desktop-is-the-target-end.md) — **⚠️ 目标端是 `apps/desktop` 桌面端,不是浏览器**（requirement.md 早写了我却跨会话遗忘）；端到端验证是硬要求、要在桌面端实机跑；我能自助起 server/daemon（别推"够不到"），只在交互式授权时找用户；daemon 必须随 server 重建
- [contract-is-dialect-of-the-project](2026-06-11-contract-is-dialect-of-the-project.md) — **双契约正确实现（设计 ③ 补齐）**：契约是 agent 响应不是 DB 字段；**格式属于工程方言不属于 multica**（harness vs medeo-market 契约标题不同为证）——prompt 只给"双契约思路 + 先读本工程既有契约对齐方言",**绝不内置模板**（单测断言 persist prompt 不含硬编码 `施工契约/验收契约`）。前置地基：planning/subtask context 加 `ProjectID`，claim 三块统一走 `attachProjectContext` surface `ProjectResources`，让规划/执行/持久化 agent 都跑在工程 repo 里才能参照方言
- [repo-ssot-persist-and-judgment-landed](2026-06-11-repo-ssot-persist-and-judgment-landed.md) — **后端落地（已写码+实机机制验证）**：`goal_persist` 一键持久化（照 goal_summary 模式，claim 额外带 ProjectResources 让 agent 在 repo 写，slug 用 created_at 派生保证快照覆盖）+ `goal_decision` 下一步判断（失败边不再无脑 cascade-block，派判断任务给总控；proceed/reshape/abort 复用 intervention；无总控/未 decide → 退化成旧 block 行为，fail-safe）；FK-less workspace 解析两个新 type 都登记（前两轮中招两次的坑）；保留"总控"只清 user-facing PMO 文案；worktree 并行 + 双契约 DB 承载 + 执行历史视图留下轮
- [design-repo-ssot-task-env](../design-repo-ssot-task-env.md) — **方案设计（已落地见上条）**：框架翻转——multica = 跨工程多 Agent 任务管理工具，适配 `dev-roleplay-harness` 结构（每工程读 `.claude/agents/` 角色 + `docs/task/` 任务历史）。SSOT 边界：平台 DB 是主真相，**repo 沉淀 = 按需一键、快照式**（不双向同步）；聊天流+调度态留平台，目标/施工图双契约/里程碑/决策/memory 一键写 repo。执行模型修正：主会话（总控）在**失败/部分/高风险边**做「下一步判断」（派判断任务给总控，非 fire-and-forget，非数据中转，非 memory 黑板——memory 是按日沉淀不是交接介质）。施工图先行：规划产出 harness 式双契约。本轮**不做 worktree 并行**（留下一轮）；**保留"总控"不改名**。后端永不调 LLM、server 不碰 repo（agent 在 daemon 机器写）

## 配套文档

- `../NEXT-ui-polish.md` — **⭐ 下一轮起始文档：任务模式 UI 优化**（清空上下文后从这里开始）
- `../requirement.md` — 需求（正文保留 06-08 原貌 + 文末 06-09 实现状态收口）
- `../design.md` — 技术方案（2.8–2.11 为实现期落地状态）
- `../design-task-mode.md` — **当前交互模型**（独立任务页 + PMO + 动态小队 + 四栏；§七点六/七点七 = 最近的 ④ 流 + PMO 收口）
- `../design-goal-ui.md` — goal 模式早期 UI 设计（被 design-task-mode 取代固定 squad/助理页开关部分）
- `../layout.md` — 布局图
