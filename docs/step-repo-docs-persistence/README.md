# 沉淀任务到工程文档

读这个文件的场景：你要把 dev-agent-harness 里的目标任务、施工图、结果、经验沉淀回工程 repo，或者要处理 repo-SSOT、双契约、工程方言。

## 本阶段内容

- 本文件：repo-SSOT、`goal_persist`、双契约、工程方言、文档分层规则。
- [`cases/260608-assistant-enhancements.md`](./cases/260608-assistant-enhancements.md)：本任务沉淀出来的工程文档案例。

## 分层

文档不是越多越好，关键是每层职责别混。

```text
docs/step-{stage-action}/      跨任务阶段手册：验证、启动、排查、编排、沉淀
docs/task/{task-id}/           单个任务的需求、方案、计划、进度
docs/task/{task-id}/memory/    证据层：一条事实一文件，按时间保留
```

不要在任务子目录里再做跨任务索引。任务目录是证据和产物，不是入口路由。

## repo-SSOT 边界

- DB 是运行态主真相：聊天流、调度态、task 状态、实时消息留在平台。
- repo 是快照投影：用户点“持久化到工程”后，把目标、施工图、里程碑、结果、memory 写进工程。
- server 不碰 repo。写 repo 的动作必须由 agent 在 daemon 机器上执行。
- persist 是一键、按需、快照式，不做双向同步。

## goal_persist 规则

`goal_persist` 是 FK-less context task，模式接近 `goal_summary`。

关键约束：

- 上游工程必须有 `local_directory`。
- claim response 必须带 `ProjectID + ProjectResources`，否则 agent 没有 repo 工作目录。
- slug 用 `goal_run.created_at` 派生，重复点击覆盖同一快照目录。
- prompt 约束 agent 只写 `docs/task/{slug}/`，不改源码、不 commit、不写凭证。
- `can_persist` 和 `persist_task_id` 要在 goal response 里可见，方便 UI 判断状态。

## 双契约

双契约是 agent 的规划响应，不是 DB schema。

不要给 multica 内置固定模板。不同工程有自己的契约方言：

- 有的工程写 `施工契约 / 验收契约`。
- 有的工程写 `实现契约（Coder 输入） / 验收契约（Evaluator 输入）`。
- 有的工程还会加剩余风险、调研结论等段落。

正确做法：

1. 规划、执行、持久化 agent 都要跑在目标工程 repo 里。
2. prompt 只说明“双契约思想”，并要求 agent 先读本工程既有 `docs/task/*/plan/*.md`。
3. 有工程方言就复用工程方言；没有才退化到通用 construction/acceptance 结构。
4. prompt 不要硬编码具体中文标题。

## 新经验怎么沉淀

1. 先写 task memory，保留上下文和证据。
2. 如果它改变未来操作，再补到对应二级阶段目录。
3. 如果只是一次事故，不要包装成项目原则。

## 证据

- [`repo-ssot-persist-and-judgment-landed`](../task/260608-assistant-enhancements/memory/2026-06-11-repo-ssot-persist-and-judgment-landed.md)
- [`contract-is-dialect-of-the-project`](../task/260608-assistant-enhancements/memory/2026-06-11-contract-is-dialect-of-the-project.md)
- [`two-layer-roles-and-repo-ssot`](../task/260608-assistant-enhancements/memory/2026-06-08-two-layer-roles-and-repo-ssot.md)
- [`design-repo-ssot-task-env`](../task/260608-assistant-enhancements/design-repo-ssot-task-env.md)
