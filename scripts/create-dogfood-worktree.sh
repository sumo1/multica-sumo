#!/usr/bin/env bash
set -euo pipefail

task_name="${TASK:-${1:-}}"
if [ -z "$task_name" ]; then
  task_name="self-dogfood"
fi

repo_root="$(git rev-parse --show-toplevel)"
base_ref="${BASE_REF:-HEAD}"
timestamp="$(date '+%Y%m%d-%H%M%S')"

slug="$(printf '%s' "$task_name" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g; s/-\{2,\}/-/g; s/^-//; s/-$//')"
if [ -z "$slug" ]; then
  slug="self-dogfood"
fi

branch="${BRANCH:-codex/dogfood-${slug}-${timestamp}}"
worktree_root="${WORKTREE_ROOT:-$(dirname "$repo_root")}"
target_dir="${DOGFOOD_WORKTREE_DIR:-${worktree_root}/dev-agent-harness-${slug}-${timestamp}}"

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

echo "==> Creating candidate worktree"
echo "    control:   ${repo_root}"
echo "    target:    ${target_dir}"
echo "    branch:    ${branch}"
echo "    base ref:  ${base_ref}"

git -C "$repo_root" worktree add -b "$branch" "$target_dir" "$base_ref"

(
  cd "$target_dir"
  bash scripts/init-worktree-env.sh .env.worktree
)

cat <<EOF

Candidate worktree is ready.

Use it as the target workspace, not as the control plane:

  cd "${target_dir}"
  make setup-worktree
  make start-worktree

Then attach this path as the project local_directory in the stable control plane:

  ${target_dir}

Do not attach or restart the control checkout while it is dispatching this work.
EOF
