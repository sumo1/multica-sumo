# Docs Entry

先按“任务实现流程里的阶段”找资料，不按任务名、日期、`decision` 这种抽象分类找。

## 阶段入口

| 你现在处在哪个阶段 | 先读 |
|---|---|
| 要准备工程角色、成员池、总控可用资源 | [`project-role-sync/`](./project-role-sync/) |
| 要设计或修改多智能体任务编排、DAG、summary、verify、上下游传递 | [`multi-agent-orchestration/`](./multi-agent-orchestration/) |
| 要做桌面端端到端验证，包含启动 server/daemon、Claude Code 子进程、computer-use、运行时排查 | [`e2e-testing/`](./e2e-testing/) |
| 要把任务结果沉淀回工程 repo，处理 repo-SSOT、双契约、工程方言 | [`repo-docs-persistence/`](./repo-docs-persistence/) |
| 要查某条结论的原始时间线和证据 | [`task/260608-assistant-enhancements/memory/README.md`](./task/260608-assistant-enhancements/memory/README.md) |

不要先从 `task/260608-assistant-enhancements/requirement.md` 或旧 `design.md` 开始。它们保留历史，但不是当前行动入口。

## 分层规则

- `docs/{stage-action}/`：二级阶段入口，目录名用英文 kebab-case。后续模型遇到“我要验证 / 我要编排 / 我要同步角色 / 我要沉淀”时从这里进。
- `docs/{stage-action}/cases/`：具体任务沉淀案例。同一个任务可以在多个阶段留下不同案例。
- `docs/task/{task-id}/`：任务级记录。放需求、方案、计划、任务历史，不负责跨任务导航。
- `docs/task/{task-id}/memory/`：证据层。一条事实一文件，按时间保留原始判断、踩坑、验证结果。

不建宽泛的 `decisions/` 目录，也不建 `actions/` 这种三级杂物间。一个判断如果只在“端到端测试”阶段有用，就放进 `docs/e2e-testing/`；如果只在“工程文档沉淀”阶段有用，就放进 `docs/repo-docs-persistence/`。目录名必须能回答“我现在要去哪看”。

## 其他资料

| 你要做什么 | 先读 |
|---|---|
| 理解产品旧全貌 | [`product-overview.md`](./product-overview.md)，注意它是旧快照 |
| 改 UI 基础规范 | [`design.md`](./design.md) |
| 改 analytics / 埋点 | [`analytics.md`](./analytics.md) |
| 改 docs 站 | [`docs-rewrite-plan.md`](./docs-rewrite-plan.md)，`docs-outline.md` 偏旧 v1 |
| 查 timezone / usage rollup 的历史 RFC | [`timezone-architecture-rfc.md`](./timezone-architecture-rfc.md) |
| 查 Codex sandbox 踩坑 | [`codex-sandbox-troubleshooting.md`](./codex-sandbox-troubleshooting.md) |

## 新增文档规则

1. 新踩坑先写 `docs/task/{task-id}/memory/YYYY-MM-DD-topic.md`，保留背景、结论、证据和被否决方案。
2. 如果它会改变未来操作，把对应二级阶段目录补一段。
3. 不要把 daily memory 改成漂亮总结；原始证据不要动。
