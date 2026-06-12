# 案例：260608 assistant-enhancements

这个案例沉淀的是从“助理直聊”演进到“目标任务 + 总控 + 动态 DAG + summary”的编排模型。

## 场景

用户希望多智能体不是固定 squad，而是围绕目标动态设计工作流。multica 不能照搬 Claude Code 那种单进程轻量 spawn 模式，因为这里每个节点是真实本机 CLI 进程，成本和隔离边界都不同。

## 关键教训

- 后端不直接调 LLM；规划、执行、汇总、判断都派 task 给 agent。
- goal 模式使用 `goal_run` / `goal_subtask`，不要扩展旧 autopilot。
- 成员池是可调用角色库，不是固定流程。总控根据目标设计 DAG。
- verify 节点是第一类动态 workflow：`execute → verify → pass/reject`，reject 有界重跑，没 verdict fail-open。
- planning task 不负责最终交付；最终交付来自 `goal_summary` task。
- 下游 execute 节点必须拿到上游 result，不要让它重推导。
- FK-less goal 任务必须接入共享 workspace resolver，否则 WS 流和完成钩子会静默丢失。

## 证据

- [`goal-tables-not-autopilot`](../../task/260608-assistant-enhancements/memory/2026-06-09-goal-tables-not-autopilot.md)
- [`llm-decompose-via-leader-task`](../../task/260608-assistant-enhancements/memory/2026-06-09-llm-decompose-via-leader-task.md)
- [`dynamic-workflows-verify-nodes`](../../task/260608-assistant-enhancements/memory/2026-06-09-dynamic-workflows-verify-nodes.md)
- [`pmo-summary-closeout`](../../task/260608-assistant-enhancements/memory/2026-06-10-pmo-summary-closeout.md)
- [`upstream-output-handoff`](../../task/260608-assistant-enhancements/memory/2026-06-11-upstream-output-handoff.md)
- [`execution-output-visibility`](../../task/260608-assistant-enhancements/memory/2026-06-10-execution-output-visibility.md)
