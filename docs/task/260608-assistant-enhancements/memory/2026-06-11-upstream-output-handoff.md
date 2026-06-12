---
name: upstream-output-handoff
description: 修最初就提的"结果传递"bug——execute 子任务原来零上游输入,只能重新推导上游产出;现在 depends_on 的上游 result 作为 upstream_output 喂进下游 prompt
metadata:
  type: project
---

实机发现的真 bug(用户截图):3 节点链(剖析本质→生成名→定稿)里,**节点2 找不到节点1 的产出,只能自己翻代码/设计文档重新推导工程本质**;节点3 被迫去翻 issue 评论凑齐上游产出。根因:`dispatchSubtask` 给 execute 节点的 context **只有自己的 spec,零上游输入**——只有 verify 节点(`buildReviewTarget`)才拿上游 result。这正是这个长任务**最最开始**用户提的"一个任务的结果没作为第二个 agent 入参传进去"那条线,一直没真正闭环(后来只做了失败边 `goal_decision`,成功边数据流从没接上)。

## 修法(用户选 C:默认直传,扇入/关键边才总控汇总)

- **A 已做(闭环核心)**:`buildReviewTarget` 泛化成 `buildUpstreamOutput(st, includeSpec)`——verify 用 `includeSpec=true`(要拿 spec 来判),execute 用 `false`(下游只关心上游**产出**不关心它怎么被指示)。`dispatchSubtask`:execute 节点若 `len(DependsOn)>0` → `payload.UpstreamOutput = buildUpstreamOutput(...)`(拼 depends_on 上游的 title+result),同时生成 `payload.HandoffBrief` 明确"当前目标 + 上游输入已注入 + 不走中转文件"。daemon Task + claim + `buildGoalSubtaskPrompt` 拆成 `Runtime handoff` / `Upstream input` 两段。注入位置在 spec 之后、instructions 之前。
- **B 没做(决策)**:扇入(多 dep)/关键边走"总控写交接简报再喂下游"是 **token 优化非正确性缺口**——`buildUpstreamOutput` 已正确处理多 dep,原始 result 拼接是对的,只是扇入时可能大/散。按 pragmatism 不加每条成功边的 LLM hop。留作扇入优化轮。

## 落点

- `service/goal.go`:`buildUpstreamOutput`(泛化自 buildReviewTarget)、`GoalSubtaskContext.UpstreamOutput/HandoffBrief`、`dispatchSubtask` 填充(execute+有 dep)。
- `daemon/types.go` + `handler/agent.go`:`GoalUpstreamOutput/GoalHandoffBrief` 字段;`handler/daemon.go` subtask claim 映射;`daemon/prompt.go` `buildGoalSubtaskPrompt` 注入。
- `handler/goal.go` + `packages/core/api/schemas.ts` + `packages/views/tasks/components/tasks-page.tsx`:从最新 execution task context 反解 `upstream_output/handoff_brief`,API 兼容默认空字符串,子任务输出页顶部直接展示交接摘要和上游输入。

## 验证

- Go:`TestGoalDownstreamReceivesUpstreamOutput`(root 无 upstream_output/handoff_brief;A 完成后 B 的 task context 带 A 的真实 result + 上游节点名 + 当前目标交接摘要)、`TestBuildGoalSubtaskPromptUpstreamOutput`(prompt 含 Runtime handoff、Upstream input、上游产出,并明确不使用 intermediate handoff file;root 不含)。
- TS:`GoalSubtaskSchema` 默认/保留 `upstream_output` 与 `handoff_brief`,防旧后端字段缺失导致页面崩。
- 实机:server 重建(multica 库)+ **daemon rebundle 重启**(本轮改了 prompt,[[realmachine-daemon-must-rebuild]]);桌面端触发新任务即可见节点2 直接用节点1 产出。

关联:[[which-model-ran-it-attribution]]、[[failure-intervention]]、[[restart-server-correct-db-and-proxy]]、[[desktop-is-the-target-end]]。
