---
name: node-spec-topology-internal
description: 规划时节点 spec 不许暴露 DAG 拓扑（seq1/seq2/节点号/上下游），要用语义化输入名；上游产出靠 depends_on + upstream_output 传，不靠 spec 里指代节点
metadata:
  type: project
---

`buildGoalPlanningPrompt`(server/internal/daemon/prompt.go)新增约束:**节点 spec 必须自包含,不暴露工作流拓扑**。

## 规则

- node `spec` 里**禁止**出现 `seq1`/`seq2`、节点号、依赖边、upstream/downstream、"上一个/下一个节点"这类指代。改用**语义化输入名**:"core mechanism explanation"、"accepted API contract"、"review constraints"。
- 要消费上游产出 → 在 `depends_on` 声明那个 producer(平台经 `upstream_output` 把它的真实产出喂进下游 prompt,见 [[upstream-output-handoff]]);不靠 spec 里写"读 seq1 的结果"。
- synthesis 节点若同时需要 producer 产出 + verifier 裁决 → **同时 depends_on producers 和 verifier**,别把必需的源材料藏在"只依赖 verifier"后面(否则下游拿不到真正要用的产出)。
- verify 节点 spec 也只写语义化的 review 标准,不写节点号。

## 为什么

节点 spec 是执行 agent 收到的全部指令,它**看不到别的节点**。spec 里写"参照 seq1"对执行者毫无意义(它不知道 seq1 是什么),还会泄漏调度内部结构。语义化输入名 + `depends_on` 驱动的 `upstream_output` 注入,才是干净的交接。这也呼应截图 bug:节点 2 spec 写"阅读节点1简报"但平台没传 → 它瞎找。现在 spec 说"基于 core mechanism explanation",平台经 depends_on 把节点1产出塞进来。

## 落点

`buildGoalPlanningPrompt` 的设计指令段 + 示例 JSON(最终节点 `depends_on:[1,2]` 同时依赖 producer 和 verifier,spec 用"accepted API contract and review constraints"而非节点号)。纯 prompt 约束,无代码逻辑改动。

关联:[[upstream-output-handoff]](depends_on→upstream_output 的实际数据流)、[[planning-roster-widened-to-workspace-pool]]、[[roster-carries-role-descriptions]]。
