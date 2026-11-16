#!/bin/sh
# The git steps of `make worktree NAME=<name>`, run under a machine-wide lock so
# parallel runs create worktrees one at a time: two at once collide on the
# .git/config lock and silently skip the core.hooksPath and node_modules steps.
# Usage: worktree.sh <name> <main checkout>
set -eu
name=$1
main=$2
cd "$main"
. "$main/scripts/lib/machine-lock.sh"
machine_lock_reexec HELMCENTRAL_WORKTREE_LOCK_HELD "${HELMCENTRAL_WORKTREE_LOCKFILE:-/tmp/helmcentral-worktree.lock}" worktree "make worktree" sh "$main/scripts/worktree.sh" "$name" "$main"

dir=../$(basename "$main")-$name
git fetch origin
git worktree add --no-track -b "$name" "$dir" origin/main
git -C "$dir" config core.hooksPath .githooks
ln -s "$main/frontend/node_modules" "$dir/frontend/node_modules"
