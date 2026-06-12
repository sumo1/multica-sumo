---
name: planning-roster-widened-to-workspace-pool
description: 修"任务拆解后所有子任务都派给同一个 agent"——任务的动态 squad 常只有 leader，规划 roster 现扩出全工作区可用 agent 作为可调用池；绑工程时优先 repo 的 harness 角色
metadata:
  type: project
---

实机 bug(用户截图):命名任务拆成 4 节点,**全派给同一个 E2E Agent(leader)**,而工作区明明有 8 个角色(coder/code-reviewer/evaluator/task-designer…)。总控自己在交付里点破:"squad 只有你一个 agent……当前 roster 无第二人选"。

## 根因(整条链)

- `CreateTask`(service/goal.go)建动态 squad 时**只放 leader + 用户显式勾选的成员**;用户没勾 → squad 只有 leader。
- `buildSquadRoster`(handler/squad_briefing.go)**只列 squad 成员** → 规划 roster 只有 leader,明写 "you are the only member"。
- 规划 prompt 让总控"从 Squad Roster 选角色" → 只能选自己 → 全派自己。
- 注释谎称"PMO will pick from the workspace during planning"——**假的**,PMO 根本看不到工作区其他 agent。

## 关键发现:dispatch 不要求 squad 成员

`dispatchSubtask` 只校验 assignee 是**非归档 + 有 runtime 的工作区 agent**,**不要求 squad 成员**。所以"扩 roster 视野"就足以让多角色分派工作,无需改 squad 数据。

## 修法(用户认可 B+C,不强制分派)

- **B(核心)**:新增 `Handler.buildPlanningRoster(squad, projectID)`——包 `buildSquadRoster` + 追加 "Available workspace roles" 池(全工作区非归档+有 runtime、且不在 squad 的 agent)。**只用于 goal 规划**(daemon.go 规划 claim 块),不动 issue-squad briefing(那是另一套固定 squad 委派)。规划 prompt 改:"从 Squad Roster OR 工作区池选角色(池 agent 无需是 squad 成员),按专长分派,别全堆给自己"。
- **C(精准,无 schema 改)**:goal 绑 project 时,`RoleSyncService.ProjectRoleNames(ws, project)`(新增,复用 resolveProjectDir+ScanRoleDir 扫 repo `.claude/agents/`)按**名字**匹配池内 agent,标 "(this project's role)" 并前置——总控优先用本工程的 harness 角色。未绑则纯工作区池。
- **不强制**:总控仍可按任务复杂度自主决定(简单任务一个 agent 也行),关键是它**有得选**。

## 落点

- `service/role_sync.go`:`ProjectRoleNames`(导出)。
- `handler/squad_briefing.go`:`buildPlanningRoster`(method,B+C);`buildSquadRoster` 原样不动(issue briefing 仍用)。
- `handler/daemon.go`:规划 claim 改调 `buildPlanningRoster`。
- `daemon/prompt.go`:规划 prompt 选角色那句扩到"Roster + 池 + 优先工程角色 + 别全堆自己"。

## 验证

- Go:`TestBuildPlanningRoster_WidensToWorkspacePool`(squad 只有 leader + 一个非成员工作区 agent → roster 含池、列出该 agent、写明"need NOT be squad members")。全 Go 绿(handler 那个 `TestQuickCreateIssueParentTrustBoundary` 套件内 flaky 是**既有** daemon-version test-ordering 污染——stash 改动后在干净树上同样复现,与本改无关;隔离跑过)。
- 实机:server 重建(multica 库)+ daemon rebundle 重启(prompt 改了)。E2E Test 工程重跑命名任务应见节点派给 coder/reviewer/evaluator 而非全 leader。

关联:[[upstream-output-handoff]]、[[design-member-orchestration]](角色同步入池)、[[which-model-ran-it-attribution]]、[[restart-server-correct-db-and-proxy]]、[[desktop-is-the-target-end]]。
