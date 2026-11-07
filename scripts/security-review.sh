#!/bin/sh
# Run /security-review headless against a worktree's branch.
#
#   sh scripts/security-review.sh <worktree-path>
#
# /security-review takes no argument and diffs the directory its session was
# started in, so a session in the main checkout reviews nothing of a branch
# built in a sibling worktree. This starts a fresh `claude -p` session inside
# the worktree instead. The review sees committed work only (origin/HEAD...),
# so commit first. An empty diff checked nothing, so it fails here rather than
# print a clean report.
set -eu

if [ $# -ne 1 ]; then
  echo "usage: sh scripts/security-review.sh <worktree-path>" >&2
  exit 2
fi

wt=$(cd "$1" 2>/dev/null && pwd -P) || {
  echo "security-review: $1 is not a directory" >&2
  exit 1
}

# Resolve the path against this script's own repository, wherever this was
# started: it must be a checkout of the same repo.
root=$(cd "$(dirname "$0")/.." && pwd)
common=$(git -C "$root" rev-parse --path-format=absolute --git-common-dir)
wt_common=$(git -C "$wt" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || {
  echo "security-review: $wt is not a git checkout" >&2
  exit 1
}
if [ "$common" != "$wt_common" ]; then
  echo "security-review: $wt is not a worktree of $root" >&2
  exit 1
fi

if [ "$(git -C "$wt" rev-parse --show-toplevel)" != "$wt" ]; then
  echo "security-review: $wt is not the top of a worktree" >&2
  exit 1
fi

branch=$(git -C "$wt" rev-parse --abbrev-ref HEAD)
# /security-review diffs origin/HEAD..., so the guard checks that same ref.
base=origin/HEAD
git -C "$wt" rev-parse --verify --quiet "$base" >/dev/null || {
  echo "security-review: $base does not resolve in $wt; run git fetch origin" >&2
  exit 1
}
ahead=$(git -C "$wt" rev-list --count "$base"..HEAD)
if [ "$ahead" -eq 0 ]; then
  echo "security-review: $branch has no commits ahead of $base, so the diff is empty and a review would check nothing. Commit first (git fetch origin if $base is stale)." >&2
  exit 1
fi
if git -C "$wt" diff --quiet "$base"...HEAD; then
  echo "security-review: $branch has $ahead commit(s) ahead of $base but they change nothing, so a review would check nothing." >&2
  exit 1
fi

command -v claude >/dev/null || { echo "security-review: the claude CLI is required" >&2; exit 1; }

echo "security-review: reviewing $branch ($ahead commit(s) ahead of $base) in $wt"
cd "$wt"
claude -p "/security-review"
