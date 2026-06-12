# 同步工程角色

读这个文件的场景：你要让任务模式使用工程 repo 里的 `.claude/agents`、`roles` 或 `agents`，或者排查成员池、总控、同步 agent 的 runtime/env。

## 本阶段内容

- 本文件：工程角色同步、成员池、runtime/env 约束、最小验证链路。
- [`cases/260608-assistant-enhancements.md`](./cases/260608-assistant-enhancements.md)：本任务沉淀出来的角色同步案例。

## 核心判断

角色来源是工程 repo，平台是投影。

```text
project.local_directory
→ 扫 .claude/agents/*.md / roles/*.md / agents/*.md
→ RoleSyncService 幂等创建或更新 Agent
→ 任务页成员池选择这些 Agent
→ 总控规划 DAG 并派发给对应角色
```

不要把平台里的 agent 配置当成唯一真相。工程角色文档是可沉淀、可复用、可 review 的部分。

## 同步规则

- `.claude/agents/*.md`：读 frontmatter，必要时解引用完整角色描述。
- `roles/*.md` / `agents/*.md`：可作为散文角色源。
- 按 name 幂等创建/更新，repo → 平台单向同步。
- `goal_run.project_id` 决定目标工程。
- 任务页选择工程后，同步角色，再加入成员池或 squad。

## runtime/env 约束

同步 agent 不是只写名字。

必须处理：

- runtime 选择，优先 online runtime；
- agent `runtime_mode` 必须等于 runtime 的 `runtime_mode`；
- `custom_env` 要继承已有 agent，尤其是 `CLAUDE_CODE_*` Bedrock 配置；
- `mcp_config` 没有就 NULL，不要写 `{}`。

这些不是边角料。配错之后表现是 Claude Code 403、MCP config 报错或 daemon dispatch 失败。

## 验证

最小链路：

1. 选工程。
2. 同步角色。
3. 任务页成员池出现工程角色。
4. 总控规划时使用这些角色。
5. 子任务被派发给同步 agent，并进入 running 或产生 task message。

不需要等示例业务被 LLM 完整做完。

## 证据

- [`design-member-orchestration`](../task/260608-assistant-enhancements/design-member-orchestration.md)
- [`two-layer-roles-and-repo-ssot`](../task/260608-assistant-enhancements/memory/2026-06-08-two-layer-roles-and-repo-ssot.md)
- [`daemon-agent-env-bedrock-403`](../task/260608-assistant-enhancements/memory/2026-06-10-daemon-agent-env-bedrock-403.md)
- [`task-mode`](../task/260608-assistant-enhancements/memory/2026-06-10-task-mode.md)
