# 自举改造当前工程

读这个文件的场景：你要用 dev-agent-harness 的目标模式、DAG、多运行时和 repo 沉淀能力来修改 dev-agent-harness 自己。

## 核心判断

控制平面和被改造对象必须分离。

```text
stable control plane        candidate target workspace
当前正在调度任务的实例  →    独立 git worktree / 独立 env / 独立端口 / 独立 DB
```

控制平面负责规划、派发、观察和记录。候选 worktree 才是 agent 可以改代码、启动服务、跑端到端验证的地方。

不要让正在派发任务的 server / daemon / desktop 同时成为被改造、被重启、被 kill 的目标。

## 标准流程

0. 先做 Skill 选择。

   ```bash
   make agent-skills
   ```

   只用 Skill frontmatter 做选择；如果当前任务匹配 `dev-agent-harness-self-dogfooding`，先读：

   ```text
   .agents/skills/dev-agent-harness-self-dogfooding/SKILL.md
   ```

   Skill 负责触发和流程，脚本负责确定性执行，本文档负责解释背景和案例。

1. 先保证控制平面稳定运行。

   控制平面通常是主 checkout，使用 `.env` 和固定端口。这个实例只负责调度，不作为目标工程。

2. 创建候选 worktree。

   ```bash
   make dogfood-worktree TASK=prompt-contract
   ```

   如果任务已经有需求目录，优先让命令指向 `docs/task`：

   ```bash
   make dogfood-worktree TASK_DOC=docs/task/260608-assistant-enhancements
   ```

   `docs/task/{task-id}` 是需求和任务资料入口；脚本会用目录名生成 branch / worktree slug，并在输出里提醒后续 goal 读取这个目录。

   这会创建一个 sibling worktree，分配独立 branch，并生成 `.env.worktree`。

3. 在候选 worktree 里启动候选实例。

   ```bash
   cd ../dev-agent-harness-prompt-contract-*
   make setup-worktree
   make start-worktree
   ```

   `make start-worktree` 只使用候选 worktree 的 `.env.worktree`。

4. 在控制平面里创建或选择目标 project。

   project 的 `local_directory` 必须指向候选 worktree 路径，不要指向控制平面 checkout。

5. 用 goal 模式派发任务。

   goal 应该明确写出：

   - 目标工程是候选 worktree；
   - implement 节点只能修改候选 worktree；
   - verify 节点只能启动、停止、重启候选实例；
   - promote / merge / restart control plane 需要显式授权。

6. 通过验证后再提升。

   verify pass 后，再把候选 branch 合并回 main。控制平面是否重启是独立动作，不属于普通 execute / verify 节点。

## Goal 模板

在控制平面里创建目标时，直接写清楚边界：

```text
目标：在当前 project 绑定的候选 worktree 中实现 [具体需求]。

Skill 选择：
- 开始前运行 `make agent-skills`。
- 如果任务匹配 `dev-agent-harness-self-dogfooding`，先读对应 `SKILL.md`，再执行脚本。

需求来源：
- 如果本任务来自 `docs/task/{task-id}`，创建候选 worktree 时使用 `make dogfood-worktree TASK_DOC=docs/task/{task-id}`。
- 进入 goal 后，先读取候选 worktree 中的这个任务目录，把它作为 source requirement。

边界：
- 只能修改 project local_directory 指向的候选 worktree。
- 不要修改、停止、重启正在调度本任务的控制平面 checkout / server / daemon / desktop。
- implement 节点完成后，verify 节点只验证候选实例。
- 合并到 main、push、force push、重启控制平面都需要先请求用户授权。

交付：
- 代码改动。
- 机制测试或轻量校验结果。
- 如果改变未来操作，把经验沉淀到对应 docs/step-*。
```

## DAG 边界

| 节点 | 允许做什么 | 禁止做什么 |
|---|---|---|
| plan | 读控制平面里的任务上下文，读候选 worktree 代码 | 改控制平面 checkout |
| implement | 改候选 worktree 文件 | kill / restart 控制平面进程 |
| verify | 启停候选 server / daemon / desktop，跑机制测试和端到端验证 | 使用控制平面端口当测试对象 |
| summary | 汇总候选 worktree 的结果和证据 | 伪造验证结果 |
| persist | 写候选工程的 `docs/task/{task-id}` 快照 | 顺手改源码、commit、push |
| promote | 合并候选 branch，必要时重启控制平面 | 未经授权直接 force push 或重启控制平面 |

## 隔离要求

- **目录隔离**：agent 的工作目录必须是候选 worktree。
- **端口隔离**：候选实例使用 `.env.worktree` 生成的端口，不使用控制平面端口。
- **数据库隔离**：候选实例使用独立 `POSTGRES_DB`，共享 postgres 容器可以，不能共享同一个 DB。
- **daemon profile 隔离**：候选 daemon 使用候选 server URL 对应的 profile，不复用控制平面的 active profile。
- **project resource 隔离**：控制平面里的 project `local_directory` 指向候选 worktree。

## 需要人配合的地方

- 第一次使用时，在桌面端把候选 worktree 路径挂到 project 的 `local_directory`。
- 如果候选 daemon 需要登录、授权或 LLM 凭证，需要在候选 profile 下完成。
- 合并到 main、force push、重启控制平面，都需要明确授权。

## 失败处理

候选实例失败时，只停候选实例：

```bash
cd ../dev-agent-harness-prompt-contract-*
make stop-worktree
```

不要为了修候选实例去停止控制平面。

如果要丢弃候选 worktree：

```bash
git worktree remove ../dev-agent-harness-prompt-contract-*
git branch --list 'codex/dogfood-prompt-contract-*'
git branch -D <branch-from-list>
```

先确认没有需要保留的改动。

## 和其他阶段的关系

- 改 DAG / prompt / source material：先读 [`../step-multi-agent-orchestration/`](../step-multi-agent-orchestration/)。
- 启动候选 server / daemon / desktop：先读 [`../step-e2e-testing/`](../step-e2e-testing/)。
- 同步候选工程角色：先读 [`../step-project-role-sync/`](../step-project-role-sync/)。
- 沉淀任务结果：先读 [`../step-repo-docs-persistence/`](../step-repo-docs-persistence/)。
- 查 repo-local Skill：运行 `make agent-skills`。
