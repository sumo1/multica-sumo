# 案例：260608 assistant-enhancements

这个案例沉淀的是从工程 repo 同步角色，并让任务总控使用这些角色完成规划和派发的规则。

## 场景

任务模式需要从目标工程读取角色定义，让总控在规划时使用真实工程角色，而不是平台里手工维护的一组抽象 agent。

## 关键教训

- 角色来源是 repo，平台是投影。
- `.claude/agents/*.md`、`roles/*.md`、`agents/*.md` 都可能是角色源。
- 同步按 name 幂等，repo → 平台单向。
- `goal_run.project_id` 是目标任务和工程角色之间的关键连接。
- 同步 agent 必须继承可用 runtime/env；`custom_env` 尤其不能丢，否则 Claude Code Bedrock 403。
- 无 MCP 时 `mcp_config` 应为 NULL，不是 `{}`。
- `agent.runtime_mode` 必须和 runtime 的 `runtime_mode` 一致。

## 证据

- [`design-member-orchestration`](../../task/260608-assistant-enhancements/design-member-orchestration.md)
- [`two-layer-roles-and-repo-ssot`](../../task/260608-assistant-enhancements/memory/2026-06-08-two-layer-roles-and-repo-ssot.md)
- [`daemon-agent-env-bedrock-403`](../../task/260608-assistant-enhancements/memory/2026-06-10-daemon-agent-env-bedrock-403.md)
- [`task-mode`](../../task/260608-assistant-enhancements/memory/2026-06-10-task-mode.md)
