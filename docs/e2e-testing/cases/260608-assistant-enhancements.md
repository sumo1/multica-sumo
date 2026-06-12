# 案例：260608 assistant-enhancements

这个案例沉淀的是任务模式、动态 workflow、repo-SSOT 和角色同步在桌面端实机验证时踩出的规则。

## 场景

验证目标不是 web 页面，而是 `apps/desktop`。桌面端需要本地 server、daemon、provider CLI 子进程、任务实时流、同步角色和目标任务状态机一起跑通。

## 关键教训

- 目标端是 `apps/desktop`。浏览器只能旁路验证。
- server 要连 `multica` 数据库，不要连仓库 `.env` 里的空 `local_medeo`。
- `curl localhost` 必须加 `--noproxy '*'`，否则可能被本地代理打成 502/403。
- server 新不代表 daemon 新。改 prompt、claim context、daemon types 后必须重建并重启 daemon。
- Claude Code 子进程 403 多半是 daemon env / proxy / `custom_env` 问题，不要先归因到账号没登录。
- computer-use 是 CLI + skill，且 CLI 是 use-case driven，不是自由 `click/type` 原子命令。
- 端到端测试的通过标准是机制打通，不是等 LLM 完成一个任意业务示例。

## 证据

- [`desktop-is-the-target-end`](../../task/260608-assistant-enhancements/memory/2026-06-11-desktop-is-the-target-end.md)
- [`restart-server-correct-db-and-proxy`](../../task/260608-assistant-enhancements/memory/2026-06-11-restart-server-correct-db-and-proxy.md)
- [`daemon-agent-env-bedrock-403`](../../task/260608-assistant-enhancements/memory/2026-06-10-daemon-agent-env-bedrock-403.md)
- [`computer-use-skill`](../../task/260608-assistant-enhancements/memory/2026-06-09-computer-use-skill.md)
- [`realmachine-verification-passed`](../../task/260608-assistant-enhancements/memory/2026-06-09-realmachine-verification-passed.md)
