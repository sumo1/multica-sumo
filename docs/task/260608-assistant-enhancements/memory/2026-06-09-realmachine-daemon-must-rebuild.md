# 实机验证：goal 模式要求 daemon 二进制也重建（不只是 server）

## 背景

2026-06-09 首次实机验证 goal 自动拆解：server 用今天的二进制重启了，发了一个 auto_decompose goal。

## 现象

- server 层全对：goal 建成 `status=planning`，规划任务正确派发给 squad leader，被真实 daemon claim、跑起来、completed。
- **但 goal 永远卡在 planning，0 个子任务。**
- 查规划任务的 result：agent 说「我被触发时分配的 issue ID 是空的…workspace 里一个 issue 都没有」——它收到的是**默认 issue prompt**，去 `multica issue get <id>`，而不是我写的 `buildGoalPlanningPrompt`。

## 根因

claim 任务的 daemon 是旧二进制（homebrew `/opt/homebrew/bin/multica` v0.3.13，6-02 构建；`~/.multica/daemon.id` = 019dade2）。它没有今天的 `Task.GoalPlanningRunID` 字段和 `BuildPrompt` 的 goal_planning 分支，所以 claim 响应里的 `goal_planning_*` 字段被忽略，prompt 落到 issue 默认分支。

**server 新 + daemon 旧 = goal 模式不工作。** prompt 构建发生在 daemon 侧（`server/internal/daemon/prompt.go`），daemon 是独立编译/分发的二进制（homebrew tap + desktop 打包 + 用户 PATH）。

## 教训

goal 模式（以及任何动 `daemon/prompt.go` / `daemon/types.go` / claim 响应新字段的改动）上线时，**server 和 daemon 必须一起发**。daemon 二进制滞后会静默退化——不报错，只是用错 prompt。这本质是 CLAUDE.md「installed-app 架构，旧 desktop 会打新 server」的同类问题，但发生在 daemon executor 层。

验证 daemon 版本：`agent_runtime.metadata->>'version'`（runtime 注册时上报）。规划/子任务任务被 claim 后若行为异常，先查 claim 它的 daemon 版本。

## 证据

来源：2026-06-09 实机验证实测。架构本身被证明是通的（dispatch→claim→run→complete 全链点火），唯一缺口是执行器二进制滞后。相关 [[2026-06-09-llm-decompose-via-leader-task]]。
