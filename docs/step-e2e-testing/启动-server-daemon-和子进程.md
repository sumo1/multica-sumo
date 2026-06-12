# 启动 server、daemon 和子进程

读这个文件的场景：你要启动或排查本地 server、daemon、Claude Code / Codex provider 子进程，或者任务停在 queued / planning / 403。

## 执行模型

后端不直接调用 LLM。所有 AI 动作都是：

```text
server 写入 task
→ daemon claim task
→ daemon 按 runtime/provider spawn 本机 CLI 子进程
→ 子进程输出 task_messages / result
→ server 监听完成事件推进 goal/subtask
```

所以“启动 Claude Code 子进程”不是在 server 里调一个 SDK，而是 daemon claim 后用 provider CLI 起真实进程。

## 本地启动

server 侧：

```bash
(cd server && go build -o /tmp/multica-server ./cmd/server)
DATABASE_URL="postgres://sumo@localhost:5432/multica?sslmode=disable" /tmp/multica-server
```

daemon 侧：

```bash
pnpm --filter @multica/desktop run bundle-cli
apps/desktop/resources/bin/multica daemon start --foreground --profile desktop-localhost-8080
```

健康检查：

```bash
curl -s --noproxy '*' http://localhost:8080/health
```

## 什么时候必须重建

| 改了什么 | 要重启谁 | 症状 |
|---|---|---|
| server 路由、handler、service、DB query | server | API 404、状态不对、响应没新字段 |
| daemon `prompt.go`、`types.go`、claim response 字段 | daemon 二进制 + daemon 进程 | 子进程收到旧 prompt、goal 卡 planning |
| `package.json` exports / 前端依赖入口 | vite / electron-vite / Next dev server | import/export 解析还是旧状态 |
| env / 凭证 / proxy | daemon 及其子进程 | shell 里能跑，daemon 子进程 403 或 not logged in |

先问一句：当前进程是不是在改动前启动的？如果是，优先重启，不要改已经正确的代码。

## Claude Code 子进程 env 规则

- 不要用 `env -i` 启 daemon。它会清掉 LLM 凭证。
- 要避开本机代理坑，只 unset 代理变量，例如 `http_proxy` / `https_proxy` / `ALL_PROXY`。
- daemon 会过滤子进程的 `CLAUDE_CODE_*`。Bedrock 相关变量要通过 agent `custom_env` 重新注入。
- 同步出来的 agent 要继承同 workspace、同 runtime 的已有 `custom_env`。
- 无 MCP 时 `mcp_config` 应为 SQL NULL，不是 `{}`。
- `agent.runtime_mode` 必须等于 runtime 的 `runtime_mode`。

## queued 不认领

daemon 主要靠 task wakeup 事件触发 claim，不是可靠持续轮询。server 重启、confirm 时机不对或 WS 事件丢失时，任务可能停在 `queued`。

临时处理：重启 daemon，触发 queued sweep。长期修法应补可靠 broadcast 或周期 sweep。

## 证据

- [`llm-decompose-via-leader-task`](../task/260608-assistant-enhancements/memory/2026-06-09-llm-decompose-via-leader-task.md)
- [`daemon-agent-env-bedrock-403`](../task/260608-assistant-enhancements/memory/2026-06-10-daemon-agent-env-bedrock-403.md)
- [`realmachine-daemon-must-rebuild`](../task/260608-assistant-enhancements/memory/2026-06-09-realmachine-daemon-must-rebuild.md)
- [`stale-process-gotcha`](../task/260608-assistant-enhancements/memory/2026-06-09-stale-process-gotcha.md)
- [`restart-server-correct-db-and-proxy`](../task/260608-assistant-enhancements/memory/2026-06-11-restart-server-correct-db-and-proxy.md)
