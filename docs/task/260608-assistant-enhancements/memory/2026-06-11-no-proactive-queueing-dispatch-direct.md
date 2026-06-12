---
name: no-proactive-queueing-dispatch-direct
description: 去掉 per-agent max_concurrent_tasks 准入闸——不主动排队，任务直接派给 agent，runtime 真扛不住时让它自己的错误冒上来打印
metadata:
  type: project
---

用户决策:**不要主动排队**。不同任务派给对应 agent 后,不应因 agent"满了"而静默躺 `queued` 等空位;直接派,runtime/模型真扛不住时(rate limit / overloaded / 429)让**它自己的真实错误**经 FailTask 冒上来打印。

## 改了什么(就一处)

`service/task.go` `ClaimTask`:删掉 `running >= agent.MaxConcurrentTasks → return nil`(原 :796-806 那段 `CountRunningTasks` 准入闸)。`GetAgent` 保留——但只作"agent 还在不在"的存在性校验,不再做容量判断。

## 关键:三道"闸"只删了一道,另两道是不同职责必须留

- ❌ **删**:per-agent `max_concurrent_tasks` 准入闸——这是会"主动排队"的人为容量闸。
- ✅ **留**:`ClaimAgentTask` SQL 里的 **per-issue / per-chat / quick-create-shape 串行**——这是**正确性不变式**(同一 issue/会话同一 agent 同时只跑一个,否则两个进程抢改一个 issue / "最近 issue" 查询打架),不是容量限制。用户也明确"同一 agent 内串行没问题"。
- ✅ **留**:daemon 的 `newTaskSlotSemaphore(20)`——机器级底线(别 fork 炸主机),满了 daemon 停止轮询不报错,非用户可见排队语义。

## 错误透传链(已验证)

agent 跑挂 → `daemon.go:2403` `FailTask(taskID, result.Comment, …, failureReason)` 把 agent 的真实输出/错误作为 failure 传回 → 进 task 的 failure_reason → ④ 流可见。所以"打印模型报错"开箱即有,不用额外做。

## 验证

- `TestClaimDispatchesPastConcurrencyLimit`(cmd/server):agent `max_concurrent_tasks=1` + 两个**不同 issue** 的任务 → **两个都被 claim**(旧行为第二个返回 nil 躺队列)。用不同 issue 是关键:同 issue/同 quick-create-shape 会被串行闸挡(那是对的),只有不同 issue 能单独验证容量闸已删。
- service + daemon + 触碰区(Claim/Goal/TaskMode)全绿。cmd/server 那批 `TestCommentTrigger*` flaky 是**既有** test-ordering 污染——stash 改动后干净树同样复现,与本改无关;隔离跑全过。
- 实机:server-only 改动,重建换上(multica 库),daemon 无需 rebundle。

## 注意

- `max_concurrent_tasks` 列 + API 字段还在(建 agent 默认值、前端 echo),只是**不再用于 claim 准入**。若以后想恢复某种限流,应在派发层 + 总控做(有全局视野),不要回到末端静默排队。
- 关联:[[planning-roster-widened-to-workspace-pool]](同 agent 堆队列的真正解是派给不同角色)、[[failure-intervention]]、[[restart-server-correct-db-and-proxy]]。
