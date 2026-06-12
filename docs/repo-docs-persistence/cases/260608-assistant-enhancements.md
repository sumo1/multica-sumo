# 案例：260608 assistant-enhancements

这个案例沉淀的是 repo-SSOT、任务快照、双契约和工程方言的边界。

## 场景

multica 是跨工程多 Agent 任务管理工具。平台负责运行态，工程 repo 负责可沉淀、可 review、可复用的任务文档。任务完成后需要一键把目标、施工图、结果和经验写回 repo。

## 关键教训

- DB 是运行态主真相，repo 是按需快照投影。
- server 不碰 repo；写 repo 的动作由 agent 在 daemon 机器上执行。
- `goal_persist` 必须带 `ProjectResources`，否则 agent 没有目标工程工作目录。
- persist slug 用 goal 创建时间派生，重复点击覆盖同一快照目录。
- 双契约是 agent 响应，不是 DB 字段。
- 契约格式属于工程方言，不属于 multica。prompt 只能给原则，不能硬编码某个工程的标题模板。
- memory 是证据层，不是节点间数据黑板；上游产物要通过 task context 显式传递。

## 证据

- [`repo-ssot-persist-and-judgment-landed`](../../task/260608-assistant-enhancements/memory/2026-06-11-repo-ssot-persist-and-judgment-landed.md)
- [`contract-is-dialect-of-the-project`](../../task/260608-assistant-enhancements/memory/2026-06-11-contract-is-dialect-of-the-project.md)
- [`two-layer-roles-and-repo-ssot`](../../task/260608-assistant-enhancements/memory/2026-06-08-two-layer-roles-and-repo-ssot.md)
- [`design-repo-ssot-task-env`](../../task/260608-assistant-enhancements/design-repo-ssot-task-env.md)
