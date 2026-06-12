---
name: desktop-is-the-target-end
description: 本任务的目标端是 apps/desktop（Electron 桌面端），不是浏览器/web——实机验证必须在桌面端做，端到端验证是硬要求
metadata:
  type: project
---

**本任务（assistant-enhancements / 任务模式 / repo-SSOT）的目标端是 `apps/desktop`（Electron 桌面客户端），不是浏览器。** requirement.md:29-33、:320-323 早已写明"端形态:桌面客户端优先,不依赖浏览器。所有新功能以 `apps/desktop` 为目标端"——但它只在需求正文里,没进 memory 索引,导致我跨会话遗忘、错把 web/browser 当验证面。记此一条,杜绝再忘。

## 为什么是桌面端

- 已定**路径 C**:desktop 拉起一个本地完整 server 实例(`multica server --local-mode`),渲染进程 `apiUrl` 指向它;daemon 仍是纯 executor。功能本身不绑浏览器(`apps/web` 与 `apps/desktop` 共用 `@multica/views`),但**验证面是桌面端**。
- desktop 主进程具备本机能力:拉本地 daemon(`daemon-manager.ts`)、起本地端口、IPC(`daemon:*`)、`fix-path` 修 GUI PATH。

## 对验证方式的硬约束

- **端到端验证是硬要求**(用户明确)。改完不是只跑 `go test` 就算完——要在**桌面端实机**把链路跑通。
- **我能起服务,别推说"够不到"**:有 Bash + 后台运行,之前会话就是我起的 server/daemon。原则=我尽量自助起服务做端到端验证,**只在真需要交互式授权(登录、sudo、动用户数据)时才找用户**。
- 桌面端实机流:`pnpm dev:desktop`(Electron + electron-vite HMR)已在跑时,后端要单独起。`make server`(`cd server && go run ./cmd/server`)起中心 server;`make daemon`(`daemon restart --profile local`)起/重启本地 daemon。
- **daemon 必须随 server 一起重建/重启**——[[realmachine-daemon-must-rebuild]]:server 新 + daemon 旧 = 静默用错 prompt。[[stale-process-gotcha]]:改了 schema/exports/prompt,跑着的进程要重启才生效。
- 浏览器(browser-harness)只在桌面端实在够不到某交互时才用作旁路,不是主验证面。

关联:[[stale-process-gotcha]]、[[realmachine-daemon-must-rebuild]]、[[daemon-agent-env-bedrock-403]](实机起 daemon 的 env/凭证连环坑)。
