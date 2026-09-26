#!/bin/sh
# Removes worktrees, local branches and remote branches whose GitHub PR has
# been merged. Dry run unless CONFIRM=1.
#
# "Merged" means GitHub has a merged PR for the branch AND the branch tip is
# that PR's head commit (or already in origin/main). Ancestry alone is not
# enough: a branch just made with `make worktree` has no commits yet, so it is
# an ancestor of main too, and deleting it would take a session's fresh
# checkout. A branch with commits after its PR merged is kept.
#
# A worktree with uncommitted or untracked changes is skipped along with its
# branch: it may be another session's unfinished work (AGENTS.md, Shared
# Working Tree). main, the checked-out branch and the worktree this runs from
# are never touched.
set -eu

command -v gh >/dev/null 2>&1 || { echo "prune-merged: gh is required to tell which PRs are merged" >&2; exit 1; }

confirm="${CONFIRM:-0}"
here="$(git rev-parse --show-toplevel)"
current="$(git branch --show-current)"

git fetch --quiet --prune origin

# Worktree path for a branch, or empty.
worktree_for() {
  git worktree list --porcelain | awk -v ref="refs/heads/$1" '
    /^worktree / { path = substr($0, 10) }
    $0 == "branch " ref { print path }'
}

# 0 when the branch tip is the head of a merged PR or already in origin/main.
is_merged() {
  branch="$1"
  tip="$2"
  heads="$(gh pr list --head "$branch" --state merged --json headRefOid -q '.[].headRefOid')"
  [ -n "$heads" ] || return 1
  printf '%s\n' "$heads" | grep -qx "$tip" && return 0
  git merge-base --is-ancestor "$tip" origin/main
}

run() {
  if [ "$confirm" = 1 ]; then "$@"; else echo "  would run: $*"; fi
}

removed=0
skipped=0

for branch in $(git for-each-ref --format='%(refname:short)' refs/heads); do
  [ "$branch" = main ] && continue
  [ "$branch" = "$current" ] && continue
  tip="$(git rev-parse "refs/heads/$branch")"
  is_merged "$branch" "$tip" || continue

  wt="$(worktree_for "$branch")"
  if [ -n "$wt" ]; then
    if [ "$wt" = "$here" ]; then
      echo "skip $branch: this command is running in its worktree"; skipped=$((skipped + 1)); continue
    fi
    if [ -d "$wt" ] && [ -n "$(git -C "$wt" status --porcelain)" ]; then
      echo "skip $branch: $wt has uncommitted or untracked changes"; skipped=$((skipped + 1)); continue
    fi
  fi

  echo "$branch (merged)"
  if [ -n "$wt" ]; then
    # The node_modules link points at this checkout's real install. Remove the
    # link itself, never its target, before git removes the directory.
    [ -L "$wt/frontend/node_modules" ] && run rm "$wt/frontend/node_modules"
    run git worktree remove "$wt"
  fi
  run git branch -D "$branch"
  if git ls-remote --exit-code --heads origin "$branch" >/dev/null 2>&1; then
    run git push --quiet origin --delete "$branch"
  fi
  removed=$((removed + 1))
done

# Merged branches left only on the remote.
for ref in $(git for-each-ref --format='%(refname:short)' refs/remotes/origin); do
  branch="${ref#origin/}"
  case "$branch" in main | HEAD | origin) continue ;; esac
  git show-ref --verify --quiet "refs/heads/$branch" && continue
  is_merged "$branch" "$(git rev-parse "$ref")" || continue
  echo "$branch (merged, remote only)"
  run git push --quiet origin --delete "$branch"
  removed=$((removed + 1))
done

run git worktree prune

if [ "$confirm" = 1 ]; then
  echo "prune-merged: removed $removed, skipped $skipped"
else
  echo "prune-merged: $removed to remove, $skipped skipped. Run with CONFIRM=1 to delete."
fi
