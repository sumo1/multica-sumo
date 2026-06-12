---
name: contract-is-dialect-of-the-project
description: 双契约的正确实现——契约是 agent 响应不是 DB 字段；格式属于工程方言不属于 multica，prompt 只给思路+引导对齐工程既有契约，绝不内置固定模板
metadata:
  type: project
---

设计 ③"施工图双契约"这轮补齐。核心是**两次方向纠正**,记下来,否则极易回退成"给工具内置一套契约模板"。

## 纠正一:契约是【响应】,不是【数据结构】

我一度想给 `goal_subtask` 加结构化契约字段 / 纠结塞 `spec` 还是加列——**伪命题**。
契约就是 agent 规划时产出的 `spec` 文本(自由文本本就够装),不进 DB schema。
持久化时 agent 把 spec **忠实铺陈**成 `plan/step-*.md`——同一份内容,平台上是"总控的规划响应",落进 repo 后升格成"工程的施工/验收指导文档"。靠持久化那一下完成身份转换,零迁移。

## 纠正二(定盘星):格式属于【工程方言】,不属于 multica

multica 是**通用工具**,适配 harness 工程,不发明格式。看了真实证据:
- harness 范本 `doctrine/03-dual-contract.md`:`## 施工契约（给 coder）` / `## 验收契约（给 evaluator）`
- medeo-market(`one2x/medeo-market/docs/task/260424-market-prd-infra/plan/step1-terraform.md`):`## 【实现契约（Coder 输入）】` / `## 【验收契约（Evaluator 输入）】`,还有"剩余风险""调研结论"等范本没有的段。
**两者相似但不同 = 每个工程有自己的契约方言。** 工具硬塞一套模板就违背"适配工程"的定位。

**所以正确实现 = prompt 只给思路 + 引导对齐,绝不内置模板:**
- 规划/持久化 prompt 让 agent**先读本工程 `docs/task/*/plan/*.md` 既有契约,复用那个工程的 section 名/结构/语言**;工程没有既有契约才退化到通用 construction/acceptance 形状。
- prompt 里**不准出现** `施工契约`/`验收契约` 这种具体标题(那是某个工程的方言,不是通用真理)——单测 `TestBuildGoalPersistPrompt` 显式断言 prompt **不含**这俩硬编码标题。

## 前置地基(否则上面全落空)

"参照本工程方言"的前提是 **agent 跑在那个 repo 里**。原本只有 `goal_persist` 挂了 `ProjectResources`;
这轮给 `GoalPlanningContext` + `GoalSubtaskContext` 都加了 `ProjectID`(从 `goal_run.project_id` 透传),
daemon claim 三处统一走新助手 `Handler.attachProjectContext`(`project_resource.go`)surface `ProjectID + ProjectResources`——
daemon 靠它 `findLocalDirectoryAssignment` 定 agent 工作目录。规划/执行/持久化 agent 现在都跑在工程 repo 里。

## 落地点

- `service/goal.go`:两个 context 结构加 `ProjectID`,`dispatchPlanningTask`/`dispatchSubtask` 透传。
- `handler/project_resource.go`:新助手 `attachProjectContext`(三个 claim 块复用,消了持久化块的重复)。
- `handler/daemon.go`:planning/subtask/persist 三块都调 `attachProjectContext(&resp, ...)`。
- `daemon/prompt.go`:规划 prompt 加"DUAL CONTRACT + 对齐工程方言"(`task.ProjectID != ""` 才提方言);执行 prompt 加"按工程约定、满足 spec 验收项";持久化 prompt 从"现场推导双契约"改"先对齐工程既有 task-doc 方言 + 忠实转写已是双契约的 spec",删掉了原来硬编码的 `施工契约/验收契约` 模板。

## 讨论态"总控主动引导澄清"(设计 ③ 讨论引导那半,同轮补)

讨论会话本就是绑了 `goal_run_id` 的普通 chat,走 `buildChatPrompt`,总控只是普通应答、不主动引导。补法**完全复用 takeover 那条已有路径**:
- `ChatSession` 已有 `goal_run_id` 列;daemon claim 在 chat 分支里,若该 session 的 goal_run **`status=='discussion'`** → set `GoalDiscussionActive`(+title/goal)。一旦 confirm 进 planning,status 变,flag 自动不再 set,chat 退回普通会话。
- `buildChatPrompt` 据此注入"## 你是总控,在任务讨论阶段"框架:主动问澄清问题、把理解回述成"任务卡"、收敛(别没完没了地审问)、**不调任何 CLI、不自己 plan/dispatch、目标清晰了让用户点 Confirm**(确认是用户动作不是 agent 动作)。
- 落点:`agent.go`/`types.go` 加 `GoalDiscussionActive/Title/Goal`;`daemon.go` chat 分支加 discussion 判定(紧挨 takeover 块);`prompt.go` `buildChatPrompt` 加 facilitation 块。
- 单测:`TestBuildChatPromptDiscussionFacilitation`(discussion 才有框架、含"总控/task card/Confirm & execute"、普通 chat 没有)、`TestClaimDiscussionChatMarksGoalDiscussionActive`(discussion 态 flag=true,进 executing 后 flag 清)。

## 验证

- 单测:`TestBuildGoalPlanningPromptDialectAlignment`(有 project 才提方言、始终要双契约)、`TestBuildGoalPersistPrompt`(含"对齐工程方言"、不含硬编码契约标题)、`TestClaimGoal{Planning,Subtask,Persist}TaskCarriesRepoContext`(三类任务 claim 都带 local_directory)、讨论引导 2 个见上。
- 全 Go 套件绿。本轮纯后端+prompt,无 TS 改动。
- 关联:[[repo-ssot-persist-and-judgment-landed]](同轮前半)、[[design-repo-ssot-task-env 见 ../design-repo-ssot-task-env.md]](§三③)、[[two-layer-roles-and-repo-ssot]](repo 是 SSOT、平台是投影——契约同理)。
