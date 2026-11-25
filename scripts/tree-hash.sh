#!/bin/sh
# Print one hash for a worktree's full working state.
#
#   sh scripts/tree-hash.sh <worktree>
#
# The hash covers tracked changes, staged changes and untracked files that are
# not ignored, so two runs print the same hash only when the files are the
# same. It also covers HEAD, the newest v* tag and the merge base with
# origin/main, which the changelog and docs checks read. `make check` prints it in its summary, which lets a caller see that a
# check already ran on exactly this tree and skip running it again.
#
# It builds the tree in a temporary index (GIT_INDEX_FILE), so the real index,
# and what `git status` shows, stay as they were.
set -eu

if [ $# -ne 1 ]; then
	echo "usage: sh scripts/tree-hash.sh <worktree-path>" >&2
	exit 2
fi

wt=$(git -C "$1" rev-parse --show-toplevel 2>/dev/null) || {
	echo "tree-hash: $1 is not a git checkout" >&2
	exit 1
}

idx=$(mktemp "${TMPDIR:-/tmp}/tree-hash.XXXXXX")
trap 'rm -f "$idx"' EXIT
trap 'exit 1' INT TERM HUP
real=$(git -C "$wt" rev-parse --path-format=absolute --git-path index)
if [ -f "$real" ]; then
	# Start from the real index so git reuses its stat cache and rehashes only
	# files that changed. The real index itself is never written. -p keeps its
	# mtime: git rechecks the contents of any entry as new as the index file,
	# and a copy stamped "now" would make a same-second edit look clean.
	cp -p "$real" "$idx"
	GIT_INDEX_FILE=$idx
	export GIT_INDEX_FILE
else
	# A checkout with no index yet: build one from HEAD. git reads an empty
	# file as an index it must replace, never as a real one.
	GIT_INDEX_FILE=$idx
	export GIT_INDEX_FILE
	git -C "$wt" read-tree HEAD
fi
git -C "$wt" add -A
tree=$(git -C "$wt" write-tree)

# Checks read refs as well as files: scripts/check-changelog.sh compares with
# the newest v* tag, scripts/docs-lint-changed.sh with the merge base of
# origin/main and HEAD. Both are hashed in, as those scripts find them, so a
# new tag or a moved base changes the hash. A ref that does not exist prints
# "none", which only labels the state; the checks themselves fail on it.
head=$(git -C "$wt" rev-parse HEAD)
tag=$(git -C "$wt" tag --list 'v*' --sort=-v:refname | head -1)
if [ -n "$tag" ]; then
	tag="$tag $(git -C "$wt" rev-parse "refs/tags/$tag^{commit}")"
else
	tag=none
fi
base=$(git -C "$wt" merge-base origin/main HEAD 2>/dev/null) || base=none
# The Go tests load plugin binaries from the ignored /plugins/, so each file's
# content hash goes in as well.
plugins=$(git -C "$wt" ls-files -o -i --exclude-standard -- plugins | while IFS= read -r f; do
	printf '%s %s\n' "$(git -C "$wt" hash-object -- "$f")" "$f"
done | git -C "$wt" hash-object --stdin)
printf '%s\n' "tree $tree" "head $head" "tag $tag" "base $base" "plugins $plugins" | git -C "$wt" hash-object --stdin
