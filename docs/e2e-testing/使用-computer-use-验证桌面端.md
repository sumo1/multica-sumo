# 使用 computer-use 验证桌面端

读这个文件的场景：你要让模型操作本地桌面客户端，做端到端验证、截图、trace 或权限检查。

## 核心判断

computer-use 不是 provider，不是 MCP server，也不是和 Claude/Codex 并列的大脑。

它是本机 CLI + skill：CLI 提供桌面操作能力，skill 教 agent 什么时候、怎么调用它。

## 能力边界

computer-use CLI 是用例驱动，不是自由原子命令。

可用命令形态：

```text
computer-use version
computer-use apps
computer-use capabilities --app <app>
computer-use usecases list
computer-use usecases dry-run <id>
computer-use usecases run <id> --fake
computer-use usecases run <id> --mac-helper <path>
computer-use trace --last
```

不要写不存在的命令：

```text
computer-use click x y
computer-use type "..."
```

如果要做的动作没有用例覆盖，正确路径是先在 `computer-use-harness/usecases/cases.yaml` 增加用例，再让 agent 跑用例并读 trace。

## 验证流程

1. 检查权限和环境。macOS 14+、Node 22+、Swift 6，真跑需要 Accessibility + Screen Recording。
2. 用 `usecases dry-run` 看用例会做什么。
3. 先跑 `--fake` 验证参数和路径。
4. 真跑时指定 `--mac-helper <helper-bin>`。
5. 用 `trace --last` 读取 JSONL 轨迹，作为验证证据。

## 在 multica 里的定位

- multica 侧不需要为 computer-use 改业务代码。
- workspace skill 挂到 agent 后，agent 通过 shell 调 CLI。
- 端到端验证优先目标是 `apps/desktop`。computer-use 是补足“模型难以稳定操作桌面端”的执行工具。
- trace 目前留在本地，是否回流进任务状态栏是后续议题。

## 证据

- [`computer-use-skill`](../task/260608-assistant-enhancements/memory/2026-06-09-computer-use-skill.md)
- [`computer-use-is-mcp-plugin`](../task/260608-assistant-enhancements/memory/2026-06-08-computer-use-is-mcp-plugin.md)
- [`desktop-form-and-computer-use`](../task/260608-assistant-enhancements/memory/2026-06-08-desktop-form-and-computer-use.md)
- [`desktop-is-the-target-end`](../task/260608-assistant-enhancements/memory/2026-06-11-desktop-is-the-target-end.md)
