#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
task_name="${TASK:-${1:-}}"
task_doc="${TASK_DOC:-}"

if [ -n "$task_doc" ]; then
  case "$task_doc" in
    /*)
      task_doc_abs="$task_doc"
      task_doc_rel="${task_doc_abs#"$repo_root"/}"
      ;;
    *)
      task_doc_rel="${task_doc#./}"
      task_doc_abs="${repo_root}/${task_doc_rel}"
      ;;
  esac

  case "$task_doc_rel" in
    docs/task/*) ;;
    *) echo "TASK_DOC must point under docs/task/, got: ${task_doc}"; exit 1 ;;
  esac

  if [ ! -d "$task_doc_abs" ]; then
    echo "TASK_DOC directory does not exist: ${task_doc_abs}"
    exit 1
  fi

  task_doc="$task_doc_rel"
  if [ -z "$task_name" ]; then
    task_name="$(basename "$task_doc")"
  fi
fi

if [ -z "$task_name" ]; then
  task_name="self-dogfood"
fi

base_ref="${BASE_REF:-HEAD}"
timestamp="$(date '+%Y%m%d-%H%M%S')"

slug="$(printf '%s' "$task_name" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g; s/-\{2,\}/-/g; s/^-//; s/-$//')"
if [ -z "$slug" ]; then
  slug="self-dogfood"
fi

branch="${BRANCH:-codex/dogfood-${slug}-${timestamp}}"
worktree_root="${WORKTREE_ROOT:-${repo_root}/.dogfood-worktrees}"
target_dir="${DOGFOOD_WORKTREE_DIR:-${worktree_root}/${slug}-${timestamp}}"

if [ "${ALLOW_DIRTY:-0}" != "1" ] && [ -n "$(git -C "$repo_root" status --porcelain)" ]; then
  echo "Refusing to create a dogfood worktree from a dirty control checkout."
  echo "Commit, stash, or re-run with ALLOW_DIRTY=1 if you intentionally want this."
  exit 1
fi

if git -C "$repo_root" rev-parse --verify --quiet "refs/heads/${branch}" >/dev/null; then
  echo "Refusing to overwrite existing branch: ${branch}"
  exit 1
fi

if [ -e "$target_dir" ]; then
  echo "Refusing to overwrite existing path: ${target_dir}"
  exit 1
fi

mkdir -p "$worktree_root"

echo "==> Creating candidate worktree"
echo "    control:   ${repo_root}"
echo "    target:    ${target_dir}"
echo "    branch:    ${branch}"
echo "    base ref:  ${base_ref}"
if [ -n "$task_doc" ]; then
  echo "    task doc:  ${task_doc}"
fi

git -C "$repo_root" worktree add -b "$branch" "$target_dir" "$base_ref"

(
  cd "$target_dir"
  bash scripts/init-worktree-env.sh .env.worktree
)

cat <<EOF

Candidate worktree is ready.

Use it as the target workspace, not as the control plane:

  make -C "${target_dir}" setup-worktree
  make -C "${target_dir}" start-worktree

For isolated desktop E2E, start a separate candidate desktop:

  make -C "${target_dir}" start-desktop-worktree

Then attach this path as the project local_directory in the stable control plane:

  ${target_dir}

Do not attach or restart the control checkout while it is dispatching this work.
EOF

if [ -n "$task_doc" ]; then
  cat <<EOF

This candidate was created for requirement docs:

  ${task_doc}

When creating the goal, tell the planner to read that directory inside the
candidate worktree and treat it as the source requirement.
EOF
fi
