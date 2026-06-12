# 任务工作区

这个目录承载**复杂需求的任务级记录**：需求拆解 → 技术方案设计 → 子 Agent 串/并行执行 → memory 证据沉淀。

它不是跨任务导航入口。后续模型如果是带着动作进来，例如“我要做端到端测试”“我要启动 daemon”“我要排查 403”，先看 [`../README.md`](../README.md) 的动作入口。

思路借用 [`dev-roleplay-harness`](https://github.com/) 的双 SSOT 与三层记忆，但**不接入它的 7 角色体系**。这里只取三样东西：

1. **双契约**——每个子任务同时写「施工契约」（怎么做）和「验收契约」（怎么算做完）。
2. **可并行 = 文件范围互斥 + 约束显式 + 验收独立**——满足三条才标并行，否则串行。
3. **证据沉淀**——被否决的方案、踩坑、为什么不选另一条路，写进 `memory/`，不让它随会话蒸发。

## 一个任务的标准生命周期

```text
需求原文          细化拆解            技术方案            执行
──────────────────────────────────────────────────────────────
requirement.md → breakdown.md → design.md → plan/step-*.md → 子 Agent 施工
                                                  │
                                                  └── memory/ 记录证据与踩坑
```

| 阶段 | 产出物 | 谁来做 | 关键问题 |
|------|--------|--------|---------|
| 1. 需求录入 | `requirement.md` | 人类提，AI 整理 | 这到底要解决什么真问题？ |
| 2. 细化拆解 | `breakdown.md` | AI | 拆成哪些可独立验收的子任务？ |
| 3. 方案设计 | `design.md` | AI | 数据结构、边界、破坏性风险、串/并行依赖图 |
| 4. 执行计划 | `plan/step-N.md` | AI | 每个子任务的双契约 |
| 5. 执行 | 代码 + `memory/` | 子 Agent | 按契约施工，决策回流 |

## 目录结构

```text
docs/task/{task-id}/
├── requirement.md        # 需求原文 + AI 整理后的理解确认
├── breakdown.md          # 需求细化与子任务拆解
├── design.md             # 技术方案：数据结构、依赖图、串/并行编排
├── plan/
│   ├── step-1-xxx.md      # 子任务双契约（施工 + 验收）
│   └── step-2-xxx.md
└── memory/
    └── YYYY-MM-DD-xxx.md  # 结论、被否决方案、踩坑（一条一文件）
```

`{task-id}` 命名：`YYMMDD-{kebab-slug}`，例如 `260608-issue-bulk-actions`。

## 串行 vs 并行的判定

`design.md` 里必须画出子任务依赖图。一组子任务能标为**并行**，当且仅当三条全满足：

- **文件范围互斥**——各子任务「可改文件」清单不重叠。
- **约束显式**——每个子任务的边界写在自己的契约里，不依赖「默认规则」。
- **验收独立**——各自的验收条目只针对自己的产出，不要求「整合后才能验」。

有一条做不到，就串行。**宁可拆粗一点串行跑，也不要拆细但互相缠绕。**

## 执行映射（Claude Code 子 Agent）

- **串行链**：主会话按依赖顺序逐个 spawn 子 Agent，前一个产出作为后一个的输入前提。
- **并行组**：主会话在一条消息里同时 spawn 多个子 Agent（各自独立上下文、独立 worktree 视需要），全部回收后再进下一阶段。
- 每个子 Agent 的 prompt **必须自包含**：任务 ID、子任务 plan 文件路径、施工契约锚点。它看不到主会话历史。

## 当前任务索引

<!-- 新任务在这里登记一行：- [{task-id}](./{task-id}/) — 一句话 -->
- [260608-assistant-enhancements](./260608-assistant-enhancements/) — 任务模式 / 多智能体动态 workflow / computer-use 桌面验证 / repo-SSOT。按动作查资料先从 [`../README.md`](../README.md) 进入；原始证据看 [`260608-assistant-enhancements/memory/README.md`](./260608-assistant-enhancements/memory/README.md)。
