# 端形态澄清 + computer-use-harness 集成

## 端形态：不是新需求，是误会 + 一行配置

用户反馈「功能启动后还在浏览器里，没必要」。探查结论：

- 根因不是功能绑死浏览器。`apps/web`(Next.js, localhost:3000) 与 `apps/desktop`(Electron) **共用同一套 `@multica/views` 页面**。
- 「还在浏览器」是因为跑的是 `make dev` / `pnpm dev:web`（起 web）。用 `pnpm dev:desktop` 就在 Electron 窗口跑。
- desktop 主进程本机能力齐备：拉本地 daemon（`daemon-manager.ts` 用 `execFile`）、起本地端口、IPC（`daemon:*`）、`fix-path`。
- 收敛到「单向连本地端口」：改 `apps/desktop/src/shared/runtime-config.ts` 的 `apiUrl`/`wsUrl` 指向本地 daemon（约 3 行）。

**结论**：端形态记为全局约束（桌面优先），不是要拆解的需求。待 design 确认本地 daemon 是否暴露与 server 相同 API 面。

## computer-use-harness 集成（需求三）

位置：`/Users/sumo/workplace/opensource/computer-use-harness`。

- 定位：macOS 优先的 CLI-first 本机 computer-use runtime，机器可读 JSON。**自身也是 dev-roleplay-harness 工程**（有 docs/task、docs/engineering）→ 是「导入已有 harness 工程」的首个真实用例。
- 六个一等公民（`src/core/contracts.ts`）：Target / Observation / Action / Policy / ActionResult / Trace。policy 拦截 + JSONL trace（`.computer-use/traces/`）。
- 工具组合：8-10 个通用 Capability 自动降级链（WaitForState→Navigation→Dialog→AX抽取→ScreenshotVision→TextInput→AXElementFinder→坐标点击）。App Adapter 只给 semantic hints。
- 执行底座：Swift native helper（`native/mac-helper/`），**stdio JSON-RPC 2.0 长连接**，支持 click/type/key/scroll/listApps 等。
- 集成形态：首选 CLI 子进程 + JSON 输出（适合 Go/Electron），也可 TS API 直调 / RPC server。

## 完整集成版图

```
multica desktop (Electron, 单向连本地 daemon)
   └─ daemon (Go, 已能 spawn 子进程) ← 统一封装点
        ├─ 拉起 codex/claude-code (goal: /Goal /goal)   ← 需求二
        └─ 调用 computer-use CLI (本机 UI 操作)          ← 需求三
```

## 待 design 阶段定的点

1. 集成形态：CLI 子进程 vs 常驻 JSON-RPC（倾向先 CLI 子进程，贴合 daemon spawn 模式）。
2. 工具暴露粒度：一个高层「操作本机」工具 vs 拆细；与 codex/claude-code 自身工具系统怎么并存。
3. policy / 权限：是否再加一层平台授权闸口；trace 是否回流状态视图。
4. 引入方式：子模块 / vendoring / 按路径调用。
5. 非 mac 端降级。

## 证据来源

用户 2026-06-08 需求细化轮。探查三路：computer-use 协议、multica desktop 形态、multica runtime 后端。
相关：[[2026-06-08-two-layer-roles-and-repo-ssot]] [[2026-06-08-goal-mode-trigger]]
