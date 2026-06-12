# 编排多智能体任务

读这个文件的场景：你要改任务模式、goal/subtask 状态机、总控规划、动态 DAG、verify 节点、summary 收口或上游结果传递。

## 本阶段内容

- 本文件：多智能体任务编排的主模型、数据结构、DAG、summary、FK-less 任务约束。
- [`cases/260608-assistant-enhancements.md`](./cases/260608-assistant-enhancements.md)：本任务沉淀出来的编排案例。

## 核心模型

multica 的多智能体编排不是“后端调用一个 LLM 拆任务”。它是把每个 AI 动作都建成 task，由 daemon/runtime 执行。

```text
discussion
→ planning task 给总控
→ 总控产出 DAG / subtasks / assignee / dependencies
→ executing
→ subtask tasks 被 daemon claim
→ completed / partial / failed
→ summary task 给总控生成最终交付
```

后端只负责状态机、任务派发、上下文装配和结果同步；LLM 推理发生在 agent 子进程里。

## 数据结构

- `goal_run`：一次目标任务的主记录。
- `goal_subtask`：DAG 节点，包含 assignee、dependencies、kind、verdict、result/failure。
- task context JSONB：承载 `goal_planning`、`goal_summary`、`goal_persist`、`goal_decision` 等 FK-less 任务上下文。
- `task_messages`：agent 实时输出和最终结果的可视证据。

不要把新流程塞回旧 autopilot。goal 模式已有独立表和状态机。

## 动态 DAG

成员池不是固定流程。总控根据目标和可用成员设计 DAG，可以：

- fan-out 多个执行节点；
- 在高风险产物后插 verify 节点；
- 让不同角色做对抗验证；
- 对 trivial 步骤跳过验证，节省真实 CLI 进程和 token。

verify 节点规则：

- `kind=verify`，依赖被审 execute 节点。
- verdict `pass` 放行下游。
- verdict `reject` 让被审节点 re-arm 重跑，attempt 受限。
- 没回 verdict 时 fail-open，默认 pass + 告警。坏验证器不应该卡死交付。

## 上下游数据传递

下游 execute 节点必须拿到 `depends_on` 上游节点的 result。不要让它重新推导上游产物。

当前策略：

- 默认直传上游 result，保证正确性。
- 多依赖扇入或关键边可后续加“总控交接简报”，那是 token 优化，不是正确性前提。
- memory 不是节点间黑板；上游产物必须通过 task context 显式传给下游。

## Summary 收口

planning task 是一次性的，规划完成后自然停在“派发节点”阶段。最终交付来自 `goal_summary` task。

规则：

- 所有子任务终结后，若有成功项，派 summary task 给总控。
- summary task 读取 subtask digest，回复即最终结果，不需要 CLI 回写。
- 没有可用总控时不能把 goal 卡在 executing，要直接 finalize。

## FK-less 任务铁律

所有 context JSONB、无 issue/chat/autopilot FK 的 goal 任务，都必须接入共享 workspace resolver。不要在 handler 里自己按部分 FK 推导 workspace。

漏掉的后果通常不是显式报错，而是：

- `task:*` 事件不广播；
- `task:message` 实时流空；
- 完成钩子不触发；
- UI 只看到 persisted message，实时看不到。

## 证据

- [`goal-tables-not-autopilot`](../task/260608-assistant-enhancements/memory/2026-06-09-goal-tables-not-autopilot.md)
- [`llm-decompose-via-leader-task`](../task/260608-assistant-enhancements/memory/2026-06-09-llm-decompose-via-leader-task.md)
- [`dynamic-workflows-verify-nodes`](../task/260608-assistant-enhancements/memory/2026-06-09-dynamic-workflows-verify-nodes.md)
- [`pmo-summary-closeout`](../task/260608-assistant-enhancements/memory/2026-06-10-pmo-summary-closeout.md)
- [`upstream-output-handoff`](../task/260608-assistant-enhancements/memory/2026-06-11-upstream-output-handoff.md)
- [`execution-output-visibility`](../task/260608-assistant-enhancements/memory/2026-06-10-execution-output-visibility.md)
