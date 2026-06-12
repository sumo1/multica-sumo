# 端到端测试

读这个文件的场景：你要验证任务模式、多智能体编排、repo-SSOT、角色同步或 computer-use 相关改动，不确定该启动什么、看什么、怎样算通过。

## 本阶段内容

- [`启动-server-daemon-和子进程.md`](./启动-server-daemon-和子进程.md)：本地 server、daemon、Claude Code / Codex 子进程启动规则。
- [`使用-computer-use-验证桌面端.md`](./使用-computer-use-验证桌面端.md)：用本机 CLI + skill 操作桌面端。
- [`排查任务流和运行时.md`](./排查任务流和运行时.md)：queued、403、④ 流空、上游产出丢失、model 归属。
- [`cases/260608-assistant-enhancements.md`](./cases/260608-assistant-enhancements.md)：本任务沉淀出来的端到端验证案例。

## 核心判断

- 目标端是 `apps/desktop`，不是浏览器。浏览器只能做旁路验证。
- 端到端验证要证明真实链路通，不是等 LLM 把任意示例任务完整做完。
- “一图一测试”是最低交付：一份用户路径可视证据 + 一个机制测试。

## 启动顺序

1. 确认桌面端 dev 环境。常见入口是 `pnpm dev:desktop`，它负责 Electron + electron-vite HMR。
2. 重建并启动 server。实机数据在 `multica` 数据库，不是仓库 `.env` 里的空 `local_medeo`。

   ```bash
   (cd server && go build -o /tmp/multica-server ./cmd/server)
   DATABASE_URL="postgres://sumo@localhost:5432/multica?sslmode=disable" /tmp/multica-server
   ```

3. 重建桌面 daemon 二进制。凡是改过 prompt、claim 字段、daemon types、schema/exports，都要重建。

   ```bash
   pnpm --filter @multica/desktop run bundle-cli
   ```

4. 启动 daemon，用 desktop profile，不要误用需要交互登录的 `local` profile。

   ```bash
   apps/desktop/resources/bin/multica daemon start --foreground --profile desktop-localhost-8080
   ```

5. 健康检查必须绕过代理。

   ```bash
   curl -s --noproxy '*' http://localhost:8080/health
   ```

## 通过标准

按改动类型选判据，不要无脑等模型完成真实业务任务。

| 改动类型 | 一图 | 一测试 |
|---|---|---|
| UI / 任务页交互 | desktop 截图或 AX 树，证明目标控件、状态、输出可见 | Vitest 组件测试或状态渲染测试 |
| 后端状态机 | 用户可见状态图、日志或 DB 状态截图 | Go service / handler 测试，断言状态转移和 DB 写入 |
| daemon claim / prompt | daemon 日志或 task message 片段，证明真实子进程拿到新上下文 | prompt 单测 + claim response context 测试 |
| 实时输出 / ④ 流 | desktop 端看到 planning / subtask / summary 流 | WS / bus 测试，断言 `task:message` broadcast 发出 |
| 角色同步 / repo 上下文 | 角色入口或任务成员池截图 | 同步服务测试，断言 `.claude/agents` / `roles` 解析和幂等更新 |
| repo 持久化 | repo 中 `docs/task/{slug}` 文件列表或截图 | persist 派发 / gating / 重复快照测试 |

任务模式链路通常验证到这些信号就够了：

- 总控规划任务 `completed`。
- 子任务被派发到同步角色，并进入 `running`。
- `task_message` 实时流出现。
- claim response 带齐 `ProjectResources` / `Goal*` context。
- summary 或 persist 这类机制任务被正确派发、可被 claim。

## 常见误判

- `curl localhost` 502/403：先加 `--noproxy '*'`，不要先怀疑 server。
- daemon token 401：先确认 server 连的是 `multica` 库，不是 `local_medeo`。
- 规划 prompt 没变：server 新不代表 daemon 新，daemon 二进制要单独 rebuild。
- 桌面端看不到流：别只查 DB 是否有 message，要查 WS broadcast 是否被 workspace resolver 静默丢掉。

## 证据

- [`desktop-is-the-target-end`](../task/260608-assistant-enhancements/memory/2026-06-11-desktop-is-the-target-end.md)
- [`restart-server-correct-db-and-proxy`](../task/260608-assistant-enhancements/memory/2026-06-11-restart-server-correct-db-and-proxy.md)
- [`daemon-agent-env-bedrock-403`](../task/260608-assistant-enhancements/memory/2026-06-10-daemon-agent-env-bedrock-403.md)
- [`realmachine-verification-passed`](../task/260608-assistant-enhancements/memory/2026-06-09-realmachine-verification-passed.md)
- [`execution-output-visibility`](../task/260608-assistant-enhancements/memory/2026-06-10-execution-output-visibility.md)
