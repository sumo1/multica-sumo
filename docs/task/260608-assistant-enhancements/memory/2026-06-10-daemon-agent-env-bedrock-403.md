---
name: daemon-agent-env-bedrock-403
description: 实机跑 goal 任务时 agent 报 403 "No active subscription"/"Not logged in" 的真因——daemon 启动 env + CLAUDE_CODE_* 过滤器 + 同步 agent 的 mcp_config/runtime_mode 三连坑
metadata:
  type: project
---

实机 dogfood goal 任务时,agent 执行连环失败,排查链很长。**根因不是账号没登录,是进程启动方式 + daemon 的 env 处理。** 沉淀全部踩点,避免重复烧 token。

## 坑 1:`env -i` 启动 daemon 会清掉 LLM 凭证(我自己制造的)

为"避开 403 环境坑"我用了 `env -i HOME PATH … daemon`,把**所有** LLM 凭证环境变量
(`ANTHROPIC_BASE_URL`/`ANTHROPIC_AUTH_TOKEN`/`CLAUDE_CODE_USE_BEDROCK`/`AWS_BEARER_TOKEN_BEDROCK`…)
一起清了。daemon spawn 的 `claude` 子进程继承空凭证 → `Not logged in · Please run /login`。

- **「403 env 坑」≠「LLM 凭证」**:那个坑是**代理变量**(`http_proxy=127.0.0.1:6454` 打到 localhost:8080 变 403),
  跟 provider 凭证两码事。正解是**继承完整 env,只 unset 代理变量**(`env -u http_proxy -u https_proxy
  -u ALL_PROXY … daemon`),而不是 `env -i` 全清。
- 排查手法:`ps eww -p <daemon_pid> | tr ' ' '\n' | grep ANTHROPIC` 看进程实际持有的 env。

## 坑 2(核心):daemon 给 agent 子进程的 env 会过滤掉所有 `CLAUDE_CODE_*`

`pkg/agent/claude.go` 的 `buildEnv → mergeEnv → isFilteredChildEnvKey` **剥掉所有
`CLAUDE_CODE_*` 前缀的 env**(本意是不让 daemon 自己的 `CLAUDECODE`/`CLAUDE_CODE_*` 标记泄漏给子 agent)。
**副作用:`CLAUDE_CODE_USE_BEDROCK` 也被剥**。没有它,claude CLI 不走 Bedrock 路由,
拿 `ANTHROPIC_AUTH_TOKEN` 直连 relay → `403 No active subscription found for this group`。

- **诊断关键**:`claude -p "PONG"` 在 shell 里直接跑成功(shell 有 `CLAUDE_CODE_USE_BEDROCK=1`),
  但 daemon 子进程失败 → 差异一定在 daemon 对 env 的处理,不是凭证本身。
- **正解(平台设计的预期路径)**:per-agent **`custom_env`** 在过滤器之后 merge
  (`daemon.go` ~2711,`isBlockedEnvKey` 不挡 `CLAUDE_CODE_*`),所以把
  `{"CLAUDE_CODE_USE_BEDROCK":"1","CLAUDE_CODE_SKIP_BEDROCK_AUTH":"1"}` 写进 agent.custom_env
  就能重新注入。代码注释 `daemon.go:2705` 明说 custom_env 就是为这个用的。
- ✅ **已修**:`RoleSyncService.inheritCustomEnv` 建 agent 时,从同 workspace、同 runtime 的
  已有非归档 agent 继承 `custom_env`(优先同 runtime,否则任一非空,再否则 `{}`)。这样 Bedrock
  relay 配置自动传给同步出的角色,不再开箱即 403。(初版默认 `{}` 是病根。)

## 坑 3:同步 agent 的 `mcp_config="{}"` 让 claude CLI 报错

RoleSyncService 初版给新 agent 设 `McpConfig: []byte("{}")`。daemon 把它写进 `--mcp-config`,
但 `{}` 缺 `mcpServers` key → `claude exited: Invalid MCP configuration: mcpServers: expected
record, received undefined`。**正解:`mcp_config` 列可空,同步时传 `nil`(SQL NULL)= 无 MCP**,
daemon 就不加 `--mcp-config`。已改 + 单测断言(`TestSyncProjectRolesEndToEnd` 验 `mcp_config IS NULL`)。

## 坑 4:同步 agent 的 `runtime_mode` 硬编码 `cloud` 但分了个 local runtime

初版 `RuntimeMode:"cloud"` + 分配 workspace 第一个(local)runtime → 不匹配 → dispatch `agent_error`。
**正解:用所选 runtime 的 `RuntimeMode`**(`resolveRuntime` 返回整个 runtime 对象,优先 online)。
agent.runtime_mode 必须 == runtime.runtime_mode。已改 + 单测断言。

## 坑 5:planning/subtask 任务 queued 但 daemon 不认领

daemon 靠 WS "task wakeup" 事件触发认领,不是持续轮询。server 重启 / confirm 时机不对 →
wakeup 丢失 → 任务停在 `queued`/`started=no`。**临时解:重启 daemon 会做一次 queued 全量 sweep**。
(这是既有 infra 行为,非本次改动引入;长期应让 confirm 的 broadcast 更可靠或加周期性 sweep。)

## 验证策略教训(用户纠正)

验证「角色同步 + 任务串联」这次改动,**到"PMO 用同步角色规划成功 + 同步角色被派发执行(状态进
running、有 task_message 流)"就够了**。不必死等 LLM 把贪吃蛇真写完——那是 LLM 干活,与改动无关,
且狂烧 token。判据:planning `completed` + subtask `running` + `task_message` 有流 = 串联打通。

关联:[[realmachine-daemon-must-rebuild]](daemon 要重建)、[[realmachine-verification-passed]]
(执行引擎本身早验过)、[[2026-06-10-design-member-orchestration 见 design-member-orchestration.md]]。
