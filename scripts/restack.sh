#!/bin/sh
# Moves a stacked branch onto a new base without replaying the old base's
# commits. `make worktree NAME=<n> STACK_ON=<branch>` records two settings on the
# new branch: branch.<n>.stackedon (the branch it sits on) and branch.<n>.basemark
# (the commit it started from). A plain `git rebase` would replay everything
# between the merge base and the branch, including the base PR's own commits,
# which is wrong once that PR was force-pushed or merged. This rebases only the
# commits after the mark.
#
# Usage:
#   restack.sh <worktree> <new base branch>             fetch, rebase onto it, move the mark
#   restack.sh <worktree> --set-mark <new base branch>  record the mark after resolving a conflict
#   restack.sh <worktree> --mark                        print the mark, for a review range
#
# Exit 0 on success, 1 when the rebase stopped on a conflict (left in progress,
# old mark kept), 2 for a refusal or a usage error.
set -u

usage() {
	echo "usage: restack.sh <worktree> <new base branch>" >&2
	echo "       restack.sh <worktree> --set-mark <new base branch>" >&2
	echo "       restack.sh <worktree> --mark" >&2
	exit 2
}

[ $# -ge 2 ] || usage
wt=$1
shift
[ -d "$wt" ] || {
	echo "restack: $wt is not a directory" >&2
	exit 2
}
if [ -d "$(git -C "$wt" rev-parse --git-path rebase-merge)" ] || [ -d "$(git -C "$wt" rev-parse --git-path rebase-apply)" ]; then
	echo "restack: a rebase is in progress in $wt; finish the rebase first (git rebase --continue)" >&2
	exit 2
fi
branch=$(git -C "$wt" symbolic-ref --short -q HEAD) || {
	echo "restack: $wt has no branch checked out" >&2
	exit 2
}
mark=$(git -C "$wt" config --get "branch.$branch.basemark") || mark=

mode=restack
case $1 in
--mark)
	[ $# -eq 1 ] || usage
	mode=mark
	;;
--set-mark)
	[ $# -eq 2 ] || usage
	mode=setmark
	shift
	;;
-*) usage ;;
*) [ $# -eq 1 ] || usage ;;
esac
newbase=${1:-}

if [ -z "$mark" ]; then
	echo "restack: $branch is not a stacked branch (no base mark; start it with make worktree STACK_ON=<branch>)" >&2
	exit 2
fi

if [ "$mode" = mark ]; then
	echo "$mark"
	exit 0
fi

# record <sha>: makes <sha> the mark and clears the pending one.
record() {
	sha=$1
	git -C "$wt" config "branch.$branch.basemark" "$sha"
	git -C "$wt" config "branch.$branch.stackedon" "$newbase"
	git -C "$wt" config --unset "branch.$branch.pendingmark" 2>/dev/null
	echo "stacked on $newbase at $(git -C "$wt" rev-parse --short "$sha"): base mark $sha"
}

if [ "$mode" = setmark ]; then
	pending=$(git -C "$wt" config --get "branch.$branch.pendingmark") || pending=
	if [ -z "$pending" ]; then
		echo "restack: $branch has no pending mark; run restack.sh $wt $newbase first" >&2
		exit 2
	fi
	if ! git -C "$wt" merge-base --is-ancestor "$pending" HEAD; then
		echo "restack: the pending mark $pending is not in $branch (was the rebase aborted?); rerun restack.sh $wt $newbase" >&2
		exit 2
	fi
	record "$pending"
	exit 0
fi

if [ -n "$(git -C "$wt" status --porcelain)" ]; then
	echo "restack: the working tree in $wt has changes: commit your work on the branch first; it is not pushed until ship step 6" >&2
	exit 2
fi

git -C "$wt" fetch origin || exit 2
onto=$(git -C "$wt" rev-parse --verify -q "origin/$newbase^{commit}") || {
	echo "restack: origin/$newbase does not exist" >&2
	exit 2
}
git -C "$wt" config "branch.$branch.pendingmark" "$onto"

if ! git -C "$wt" rebase --onto "$onto" "$mark"; then
	echo "restack: the rebase stopped on a conflict; the old mark $mark is kept" >&2
	echo "restack: resolve it, git add the files, then run git rebase --continue in $wt" >&2
	echo "restack: when the rebase is done, record the mark: sh scripts/restack.sh $wt --set-mark $newbase" >&2
	echo "restack: do not rerun restack.sh $wt $newbase; the rebase has already moved the branch" >&2
	exit 1
fi
record "$onto"
