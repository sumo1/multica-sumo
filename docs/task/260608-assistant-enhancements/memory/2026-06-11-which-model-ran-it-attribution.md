---
name: which-model-ran-it-attribution
description: 任务模式显示"哪个 agent/运行时/模型响应的"——归属按任务/子任务为单位（非每消息）；model 用方案3：配置值→task_usage实际值覆盖；纯解析+渲染，零 schema 改
metadata:
  type: project
---

多运行时下用户要看"哪个 agent / 运行时 / 模型在响应"。落地了任务模式这一面(状态树 + 子任务流 + 总控会话)。聊天消息那面这轮没做。

## 关键洞察:归属按【任务/子任务】为单位,不是按消息

一个子任务的所有消息都出自它那一个 assignee agent(跑在它那一个 runtime)。总控会话出自 squad leader。所以**不给每条 task_message 打标**(TaskMessagePayload 不动),只在子任务行/流头部、总控头部显示一个归属标签即可。数据本就齐:`agent_task_queue` 有 agent_id+runtime_id、`agent.model`、`agent_runtime.name/provider`、`task_usage.model`(实际跑的)。

## model 用方案 3(用户选的):配置值 → 实际值覆盖

- 执行中:显示 agent 配置的 `model`(实时可得)。
- 任务完成 + daemon 回写 `task_usage` 后:用**实际跑的 model**覆盖(更真,可能因 fallback 与配置不同)。
- 实现:`enrichGoalResponse` 里先填 `agent.Model`,再 `GetTaskUsage(taskID)` 有值就覆盖。

## 落地(纯解析 + 渲染,零 DB schema 改)

- 后端 `handler/goal.go`:`GoalSubtaskResponse` 加 `agent_name/runtime_name/runtime_provider/model`;`GoalRunResponse` 加 `coordinator_{name,runtime_name,runtime_provider,model}`(总控=squad leader)。`enrichGoalResponse` 带 per-response agent/runtime 缓存(一个 goal 多子任务常共享 agent,别重复查)+ `actualModelForTask` 用 task_usage 覆盖。**只在 `GetGoal`/intervention 路径 enrich**(状态树轮询走 GetGoal,够了);`goalRunToResponse` 本身不 enrich。
- core `types/goal.ts` + `schemas.ts`:加字段 + zod 默认值(parse-don't-cast,老后端不返回不白屏)+ EMPTY 更新。
- 前端:`goal-status-tree.tsx` 子任务行 `agent_name · runtime · model`(优先 API 的 agent_name,回退 resolveAgentName prop)、overall-progress 头部加总控归属行;`tasks-page.tsx` SubtaskOutput 头部 + 总控 planning 流头部加归属。i18n `goal.coordinator_label` 四语言(中文=总控)。

## 验证

- Go:`TestGoalResponseExposesAttribution`(子任务带 agent/runtime/model;usage 前=配置 model,插 task_usage 后=实际 model 覆盖)。
- 前端:`goal-status-tree.test.tsx` 新增归属渲染用例;schemas.test.ts 缺字段降级 + 提供时保留。全 Go + 6 包 typecheck 绿。
- 实机:server 重建(连 multica 库,见 [[restart-server-correct-db-and-proxy]]),桌面端 + daemon 已重连;归属是纯 server 端改动,daemon 二进制无需 rebundle。用户打开任务页即可见。

## 没做(下一轮候选)

- **聊天消息**显示模型:ChatMessage/TaskMessagePayload 不带 agent/runtime/model,要单独补一层(且按"会话绑定的 agent"而非每消息)。
- 关联:[[desktop-is-the-target-end]]、[[restart-server-correct-db-and-proxy]]、[[execution-output-visibility]](④ 流来源)。
