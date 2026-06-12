# 双层角色模型 + repo 作为 SSOT（核心架构决策）

## 背景

讨论「固定职责角色 Agent 该挂在哪一层」时，纠结过 workspace 级 / project 级 / 模板+实例三种方案。用户点破：要的是双层——职责通用、规范工程维度。又进一步提出关键诉求：把已有的 dev-roleplay-harness 工程**导入**平台。这两条合起来定型了整个架构。

## 决策

### 1. 双层角色模型

- **L1 通用职责**：「我是 coder/evaluator」、Soul、边界、工具白名单。跨工程通用。**复用现有 Agent 实体（workspace 级），不新建角色模板模型。**
- **L2 工程规范**：该 repo 的约定、规范、历史沉淀。绑定具体工程。**不塞进 Agent。**
- **组合在派发时发生**：PMO 派任务 = L1 职责 + L2 规范 拼成本次上下文。

为什么拆：揉进同一个 `Agent.instructions` 会导致同一角色服务 N 个 repo 就建 N 个 agent、各抄一遍通用 Soul（改一次改 N 处）。拆开 → 各自 SSOT，组合在派发时。与 harness 自身结构同构（`roles/`=L1，`docs/engineering/`=L2）。

### 2. repo 是 SSOT，平台是投影

**不是平台定义规范再下发，而是平台从工程对应的 git repo 读取、同步、呈现。** 平台不抢 SSOT 身份，天然杜绝 harness doctrine 最反对的腐坏（平台改了但 repo 没同步）。

### 3. 导入已有 harness 工程（关键能力）

接入带 harness 结构的 repo 时，平台读约定目录同步到平台，project 维度信息瞬间补全：

```
roles/*.md 或 .claude/agents/*.md  →  角色定义（L1）
docs/engineering/                   →  project 工程规范（L2）
docs/knowledge/                     →  project 跨任务知识（L2）
docs/task/{id}/                     →  project 任务沉淀
```

## 待 design 阶段想死的点

1. **同步方向**：倾向单向 repo→平台（避免双 SSOT 漂移）。平台内编辑要么禁止，要么走「生成 diff → 人提交回 repo」。**架构承重墙，待用户最终拍板。**
2. **新鲜度**：repo 改了平台怎么刷新（初次导入 + 手动/webhook/派发时实时读）。
3. **格式契约**：认目录约定还是 frontmatter。
4. **无 harness 结构的普通 repo**：留空 / 一键初始化骨架。

## multica 现状约束

- Agent 是 workspace 级（`agent.workspace_id NOT NULL`），不存在全局/项目级 agent。证据见探查：`server/migrations/001_init.up.sql:36-49`。
- Project（工程）是 workspace 下的实体，可挂 ProjectResource（github_repo / local_directory），也可不挂。
- 要用 roleplay 派发的 project，倾向要求先绑 repo（L2 才有家）。

## 证据来源

用户 2026-06-08 需求细化轮的连续澄清。产品/架构决策，无法从代码推出。
相关：[[2026-06-08-goal-mode-trigger]]
