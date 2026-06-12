# 实机验证通过：真 LLM 上跑通完整动态工作流（含对抗验证）

## 背景

2026-06-09 首次在真 daemon + 真 claude-code 上端到端验证 goal 模式（之前只有单测/集成测试）。

## 结果：全链跑通

发了一个 goal「为 AI 任务编排 CLI 工具起名，5 候选选最佳」，auto_decompose=true。完整链路真实点火：

```
POST /api/goals → goal(planning) → 派规划任务给 leader
  → 真 claude 拆解(~2.5min) → multica goal plan 写回
  → SubmitPlan 落 DAG → executing
  → 节点① execute 跑(~4min,真 claude 头脑风暴)→ completed
  → 解锁节点② verify → 真 claude 对抗评审(~4min)
  → multica goal verdict pass 写回 → handleVerifyCompleted finalize
  → goal → completed
```

## PMO 的实际表现（超预期）

PMO（squad leader）**自主设计了带对抗验证的工作流**，没要求它验证它自己加的：

```
① [execute] 头脑风暴并推荐名字   → E2E Agent
② [verify]  对抗评审命名方案     → Hello Bot(role=reviewer), depends_on=[①]
```

- **动态结构**：把「起名」拆成「生成→对抗评审」，不是跑固定流程。
- **自主插 verify 节点**：判断命名是 taste-based、需客观评审 → 主动加对抗节点（Anthropic 文档的 generate-and-filter 模式）。
- **不同角色保独立性**：生成用 leader，评审用 squad 里 role=reviewer 的成员——正是 prompt 教的。
- **spec 质量高**：execute 给量化约束（≤8 字符、5 候选、含义来源）；verify 用「批判性对抗视角」逐项核查清单。
- **评审真去查了**：reviewer 自述「没只信自述，去 issue 读了实际产出，独立核验 registry 命名冲突」——真对抗，不是橡皮图章。最终 pass，推荐名 `baton`。

## 验证中发现并修复的两个真 bug（仅实机暴露）

1. **goal subtask / planning 任务的 task:* 事件被静默丢弃**：`ResolveTaskWorkspaceID` 对无 FK 的任务返回空 → broadcastTaskEvent 早退 → 完成钩子不触发。见 [[2026-06-09-llm-decompose-via-leader-task]]。
2. **planning 任务失败后 goal 永卡 planning**：listener 只看 goal_subtask_id，planning 任务没有 → 失败被忽略。加了 `SyncPlanningFromTask`（planning 任务终结且 goal 仍 planning → 标 failed）。单测 `TestGoalPlanningFailureFailsGoal`。

## 环境坑（非 multica bug，但要知道）

- daemon 二进制要和 server 一起重建，否则用旧 prompt。见 [[2026-06-09-realmachine-daemon-must-rebuild]]。
- 403「No active subscription」：旧 daemon 带 `CLAUDE_CODE_USE_BEDROCK=0` 快照 → claude 走没订阅的 cc-vibe relay。正确做法是 daemon 用干净 env（`env -i` + HOME/SHELL/PATH），全靠 `resolveAgentEnvViaLoginShell` 从登录 shell 拉 AI 凭据（USE_BEDROCK=1 + Bedrock relay 一起）。**坑：手动给 daemon 部分注入 AI 变量会触发 resolveAgentEnvViaLoginShell 的「已存在则跳过」逻辑，导致 USE_BEDROCK 丢失而 ANTHROPIC_BASE_URL 被采纳 → 配置撕裂 → 403。要么全给要么全不给。**

## 证据

来源：2026-06-09 实机验证。goal_run_id=c40a8bc5（已完成）。每步 status 转换 + 真 claude 产出均在 multica 库可查。
