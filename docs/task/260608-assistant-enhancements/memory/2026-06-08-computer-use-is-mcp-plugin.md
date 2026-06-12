# computer-use 接入：Skill 教学 + 本机 CLI（认知修正，三次演进）

## 最终结论（先看这条）

**computer-use 本机预装（在 PATH），multica 用一个 SKILL.md 教 agent 怎么调它的 CLI。** 不做 provider、不做 MCP server。multica 后端几乎零改动。

## 认知演进三步

| 方案 | 状态 | 原因 |
|------|------|------|
| provider（daemon Go backend 封装） | ❌ | 它是插件不是大脑，不该和 codex 并列 |
| MCP server | ❌ | 用户明确不要 MCP 服务，保持 CLI 形态 |
| Skill 教学 + 本机 CLI | ✅ | 最轻，后端零改动，computer-use 保持纯 CLI |

## 背景

需求三最初被理解为「daemon 写 Go backend 封装 computer-use」（当成 provider）。用户三次纠正：①它是插件不是 provider；②参考 codex 插件体系（一度推向 MCP）；③不要 MCP，保持 CLI，走 skill。

## 关键认知

### provider vs plugin

- **provider**（codex / claude-code / openclaw）= 谁来思考执行任务，大脑。代码硬编码，`agent.New()` switch-case，每个写一个 `Backend` 实现。
- **plugin**（computer-use）= 给大脑加一双手/工具。与 provider 正交，任何 provider 装上都能用。

把 computer-use 做成 provider 是错的。

### OpenAI 的 Codex Computer Use client 不能直接用

本机 `~/.codex/computer-use/Codex Computer Use.app`（`SkyComputerUseClient`）：
- bundle id `com.openai.sky.CUAService`，OpenAI 闭源、签名 + `embedded.provisionprofile` 的预编译 Mach-O（arm64）。
- 无源码、不可合法移植。通过 codex 私有 `notify`（turn-ended）hook 集成，非开放插件。
- **要接的是用户自己的 `computer-use-harness`（开源 CLI），不是 OpenAI 那个。**

### codex 的插件标准是 MCP

本机 `~/.codex/config.toml` 实证：codex 接外部能力全走 MCP——`mcp_servers.chrome-devtools / playwright / context7 / github / postgres`。还有 `[features] codex_hooks` + `~/.codex/hooks.json`（hooks 体系）和 `goals`（goal 模式 features 开关，配 `goals_1.sqlite`）。

## 方案：Skill 教学 + 本机 CLI（最终）

调研三条硬约束定向：
1. **skill 不能打包二进制**：`skill_file.content` 是 TEXT（migration 008），写入权限固定 `0o644` 不可执行。computer-use 含 Node CLI + Swift helper，塞不进去。
2. **agent 能自由 shell 调本机 CLI**：Claude Code 无沙箱；Codex 在 macOS 降级 `danger-full-access`（`codex_sandbox.go:15-31`）。
3. **skill = 写进 provider-native 目录的 SKILL.md**：daemon 注入 `.claude/skills/{name}/` 等（`context.go:152-214`），provider 自动发现；含 `local_skills.go` 本机 skill 自动导入机制。

**做法**：computer-use 本机预装（PATH，随 desktop 安装），写一份 SKILL.md 教 agent 怎么调它的 CLI。skill 只装「说明书」不装二进制。multica 后端几乎零改动，computer-use 保持纯 CLI。

（备查：multica 也有完整 MCP 机制 `agent.mcp_config` + Claude/Codex/OpenCode 注入——但用户明确不走 MCP。）

## 缺口

1. computer-use CLI 上本机 PATH 的分发方式（随 desktop 装 / npm link / 手动）。
2. SKILL.md 归属：workspace skill 手挂 vs `local_skills.go` 本机自动导入。
3. skill 内容粒度、policy 权限前提、trace 回流、非 mac 降级待定。

## 证据来源

四路调研 + 本机 `~/.codex/config.toml`、`~/.codex/computer-use/` 查证，2026-06-08。
相关：[[2026-06-08-desktop-form-and-computer-use]] [[2026-06-08-design-research-findings]]
