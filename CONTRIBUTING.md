# Contributing to dev-agent-harness

这份文档说明如何给 `dev-agent-harness` 做贡献。

先读：

- [`README.md`](./README.md) —— 项目定位：面向开发者的 agent harness
- [`docs/README.md`](./docs/README.md) —— 按动作阶段找资料

不要把这个工程当成原始 Multica 产品来改。当前主线是：

> 在 `dev-roleplay-harness` 基础上，把角色同步、目标编排、多运行时执行、验证闭环和知识沉淀做成一个低门槛的开发者 agent harness。

## 贡献方向

优先贡献这些方向：

- **工程角色同步**：从 `.claude/agents/`、`roles/`、`agents/` 读取角色，并同步成平台 Agent
- **目标编排**：让目标运行时拆出更好的动态任务图，正确处理 execute / verify / summary / persist / decision
- **任务合同**：让子任务只看到语义化任务合同和 source material，不泄漏 DAG 拓扑
- **多运行时执行**：让 Claude Code、Codex 等 runtime 成为同一目标里的可选执行资源
- **端到端验证**：把桌面端、浏览器、daemon、子进程验证做成 agent 可调用动作
- **repo 知识沉淀**：把目标、施工图、双契约、memory 按 harness 结构写回工程仓库
- **自举改造安全**：用独立候选 worktree 跑 implement / verify，保护正在调度任务的控制平面
- **文档整理**：把经验按动作阶段写进 `docs/step-*`，不要堆进宽泛目录

不优先：

- 原 Multica 营销文案
- 通用 issue tracker 能力
- 与 agent harness 主线无关的 UI 装饰
- 为了抽象而抽象的新框架层

## 工程边界

### 后端不直接调 LLM

所有 AI 动作都应该表现为 task：

```text
server 状态机
→ agent_task_queue
→ daemon/runtime claim
→ Claude Code / Codex / other CLI 子进程执行
→ CLI 或 task result 回写
```

不要在 server handler 或 service 里直接调用模型 API。

### server 不写用户 repo

repo 文件读写必须由 agent 在 daemon 所在机器执行。

原因很简单：server 可能是远端服务，它不一定能访问用户本地工程目录。

`goal_persist` 这类能力应该派发给 agent，让 agent 在目标工程的 `local_directory` 下写文件。

### 平台是运行态，repo 是沉淀态

平台 DB 保存：

- 聊天流
- task 状态
- 实时消息
- 调度状态

工程 repo 保存：

- 目标
- 施工图
- 双契约
- 关键判断
- memory

不要做隐式双向同步。repo 沉淀是按需快照。

### 自举改造必须隔离

用 dev-agent-harness 改 dev-agent-harness 自己时，控制平面和目标工程不能是同一个 checkout。

- 控制平面：稳定实例，负责调度、观察、记录，不被普通任务重启。
- 候选 worktree：agent 可以改代码、启动服务、跑验证、丢弃重来。
- 合并、push、重启控制平面必须显式授权。

入口见 `docs/step-self-dogfooding/` 和 `make dogfood-worktree TASK=...`。

### 子任务 prompt 不暴露 DAG

执行节点不要看到：

- `seq1`
- `seq2`
- upstream / downstream node
- previous / next node

执行节点应该看到：

- task contract
- task inputs
- source material
- acceptance criteria

## 文档规则

新增经验先判断它属于哪个动作阶段：

| 场景 | 写到 |
|---|---|
| 多智能体编排、DAG、verify、summary、source material | `docs/step-multi-agent-orchestration/` |
| 工程角色同步、成员池、runtime/env | `docs/step-project-role-sync/` |
| 任务沉淀回 repo、双契约、工程方言 | `docs/step-repo-docs-persistence/` |
| 桌面端 / daemon / computer-use / 子进程验证 | `docs/step-e2e-testing/` |
| 某次任务的原始判断、踩坑、证据 | `docs/task/{task-id}/memory/` |

规则：

1. 新踩坑先写 task memory，保留背景、结论和证据。
2. 如果它会改变未来操作，再上浮到对应 `docs/step-*`。
3. 不要新建 `decisions/`、`actions/`、`notes/` 这种泛目录。
4. 文档目录名要回答“我现在要做这个动作，该去哪看”。

## 本地开发

常用入口：

```bash
make dev
```

常用检查：

```bash
pnpm typecheck
pnpm test
make test
make check
```

后端 targeted test 通常在 `server/` 下跑：

```bash
cd server
go test ./internal/daemon ./cmd/server -run 'TestBuildGoal|TestGoal'
```

如果改了 daemon claim 字段、prompt、runtime config 或生成类型，记得重建桌面 daemon：

```bash
pnpm --filter @multica/desktop run bundle-cli
```

## 端到端验证

不要只证明“代码看起来对”。

按改动类型选择证据：

| 改动 | 最低验证 |
|---|---|
| 目标编排 / 状态机 | Go service / handler 测试，断言状态转移 |
| prompt / claim context | prompt 单测 + daemon claim 响应检查 |
| 任务页 UI | Vitest 组件测试 + 桌面端截图或 AX 树 |
| 实时输出 | WS / bus 测试，断言 `task:message` broadcast |
| 角色同步 | sync service 测试，断言角色解析和幂等更新 |
| repo 持久化 | persist 派发 / gating / 快照覆盖测试 |

实机验证优先看：

- 目标端是 `apps/desktop`
- server 是否连到真实 `multica` DB
- daemon 是否是新构建的二进制
- Claude Code / Codex 子进程是否拿到正确 prompt
- task message 是否实时显示在 UI

详细流程见 [`docs/step-e2e-testing/`](./docs/step-e2e-testing/)。

## 提交前检查

提交前至少确认：

- 没有把 `.env`、token、私有凭证写进 repo
- 没有把 `node_modules`、`.turbo`、本地缓存纳入提交
- README / docs 链接没有指向旧目录
- 新增 docs 放在正确的 `docs/step-*` 或 task memory 下
- 修改 prompt 时补对应 prompt test
- 修改状态机时补 Go 测试

## 贡献品味

这个工程不缺功能点，缺的是清晰边界。

好改动通常会让边界更直：

- 角色来源更清楚
- 任务合同更语义化
- 子任务输入更可审计
- 验证证据更可复现
- 知识沉淀更容易被下一次任务使用

坏改动通常会把东西重新搅在一起：

- server 偷偷调模型
- prompt 里泄漏 DAG 结构
- 用临时文件当隐式 handoff
- 把聊天流水倒进 repo
- 为一个特殊 case 加一层新抽象

先把数据流理顺，再写代码。
