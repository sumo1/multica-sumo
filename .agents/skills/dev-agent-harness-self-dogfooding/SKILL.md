---
name: dev-agent-harness-self-dogfooding
description: Use when a task asks to use dev-agent-harness itself, goal mode, DAG orchestration, multi-agent workflow, or multi-runtime verification to modify this repository; when a candidate worktree is needed; or when implementation/verification must not restart the active control plane.
metadata:
  short-description: Safely dogfood dev-agent-harness on itself
---

# Dev Agent Harness Self-Dogfooding

Use this skill before planning or editing when the user wants to modify
dev-agent-harness using its own goal/DAG/multi-agent workflow.

## Core Rule

Never run implementation or verification against the checkout that is currently
dispatching the task.

```text
stable control plane → candidate worktree
dispatch / observe     edit / start / stop / verify
```

## When To Use

Use this skill when:

- the user asks whether the current project can be modified through its own task mode;
- the user asks to dogfood dev-agent-harness on this repository;
- the task mentions goal mode, DAG, multi-agent orchestration, or multi-runtime execution for this repository;
- a task needs to restart server / daemon / desktop while keeping the active control plane alive;
- a project `local_directory` should point at a safe candidate target.

Do not use it for a trivial direct edit, unless the user explicitly asks to route
that edit through the harness.

## Workflow

1. Confirm you are in the stable control checkout.

   ```bash
   git status --short --branch
   ```

2. Create the candidate worktree.

   ```bash
   make dogfood-worktree TASK=<short-task-slug>
   ```

   If the work starts from an existing requirement directory under `docs/task`,
   prefer:

   ```bash
   make dogfood-worktree TASK_DOC=docs/task/<task-id>
   ```

   This derives the branch/worktree slug from the requirement directory and
   prints the doc path for the goal prompt.

   The command prints the candidate path, branch, generated DB name, and ports.

3. Move into the candidate worktree.

   ```bash
   cd <candidate_worktree_path>
   make setup-worktree
   make start-worktree
   ```

4. In the stable control plane UI, attach the candidate path as the project
   `local_directory`.

5. Create the goal with these boundaries:

   ```text
   The target project is the candidate worktree.
   Only edit the project local_directory target.
   Do not modify, stop, restart, or kill the active control plane checkout/server/daemon/desktop.
   Verify only against the candidate instance.
   Ask before merge, push, force push, or control-plane restart.
   ```

6. After verify passes, ask the user before promoting the candidate branch.

## Useful Commands

```bash
make dogfood-worktree TASK=<slug>   # run in stable control checkout
make dogfood-worktree TASK_DOC=docs/task/<task-id>
make setup-worktree                 # run in candidate worktree
make start-worktree                 # run in candidate worktree
make stop-worktree                  # run in candidate worktree
make check-worktree                 # run in candidate worktree when full verification is warranted
```

## References

Read `docs/step-self-dogfooding/README.md` when you need the full rationale,
DAG boundaries, failure handling, or goal template.
