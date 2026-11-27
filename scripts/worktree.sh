#!/bin/sh
# The git steps of `make worktree NAME=<name>`, run under a machine-wide lock so
# parallel runs create worktrees one at a time: two at once collide on the
# .git/config lock and silently skip the core.hooksPath and node_modules steps.
# Usage: worktree.sh <name> <main checkout> [<stack-on branch>]
# The new branch starts from origin/main, or from origin/<stack-on branch> for
# a PR stacked on one still open. A stacked branch records config
# branch.<name>.stackedon (the branch) and branch.<name>.basemark (the commit
# it started from); scripts/restack.sh reads and moves them. A plain worktree
# records neither.
set -eu
name=$1
main=$2
stack_on=${3:-}
cd "$main"
. "$main/scripts/lib/machine-lock.sh"
machine_lock_reexec HELMCENTRAL_WORKTREE_LOCK_HELD "${HELMCENTRAL_WORKTREE_LOCKFILE:-/tmp/helmcentral-worktree.lock}" worktree "make worktree" sh "$main/scripts/worktree.sh" "$name" "$main" "$stack_on"

dir=../$(basename "$main")-$name
git fetch origin
git worktree add --no-track -b "$name" "$dir" "origin/${stack_on:-main}"
git -C "$dir" config core.hooksPath .githooks
ln -s "$main/frontend/node_modules" "$dir/frontend/node_modules"
if [ -n "$stack_on" ]; then
	mark=$(git rev-parse "origin/$stack_on")
	git config "branch.$name.stackedon" "$stack_on"
	git config "branch.$name.basemark" "$mark"
	echo "stacked on $stack_on at $(git rev-parse --short "$mark")"
fi
