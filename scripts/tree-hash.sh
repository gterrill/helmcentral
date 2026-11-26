#!/bin/sh
# Print one hash for a worktree's full working state.
#
#   sh scripts/tree-hash.sh [--stat] <worktree>
#
# The hash covers tracked changes, staged changes and untracked files that are
# not ignored, the ignored /plugins/ files, HEAD, the newest v* tag and the
# merge base with origin/main (which the changelog and docs checks read).
# `make check` prints it in its summary, which lets a caller see that a check
# already ran on exactly this tree and skip running it again.
#
# A .gitmodules file makes it exit 2: submodules are not handled. So does a
# nested git repository (a gitlink in the index, or an untracked directory that
# is a repository, ignored ones aside): its files are not hashed, so the script
# names it and stops rather than vouch for them.
#
# It builds the tree in a temporary index (GIT_INDEX_FILE), so the real index,
# and what `git status` shows, stay as they were.
#
# `--stat` prints a second hash instead, over the same files' mtime, ctime and
# size. Writing a file moves its ctime even when the contents are put back, so
# a run can tell that a file was written although the content hash matches.
# macOS stat.
set -eu

# A caller's git location (a git hook sets some of these) must not reach any
# git call here.
unset GIT_INDEX_FILE GIT_DIR GIT_WORK_TREE GIT_OBJECT_DIRECTORY GIT_COMMON_DIR

mode=content
if [ $# -eq 2 ] && [ "$1" = --stat ]; then
	mode=stat
	shift
fi
if [ $# -ne 1 ]; then
	echo "usage: sh scripts/tree-hash.sh [--stat] <worktree-path>" >&2
	exit 2
fi

wt=$(git -C "$1" rev-parse --show-toplevel 2>/dev/null) || {
	echo "tree-hash: $1 is not a git checkout" >&2
	exit 1
}

# Submodules would need their own handling; do not guess.
if [ -e "$wt/.gitmodules" ]; then
	echo "tree-hash: $wt has a .gitmodules file; this script does not handle submodules yet" >&2
	exit 2
fi

td=$(mktemp -d "${TMPDIR:-/tmp}/tree-hash.XXXXXX")
trap 'rm -rf "$td"' EXIT
trap 'exit 1' INT TERM HUP

# Nested git repositories are not hashed: a gitlink in the index (160000) or an
# untracked directory that is a repository. Ignored ones are outside the hash,
# so they do not count. This runs before any `git add`, which would otherwise
# warn about the embedded repository.
git -C "$wt" ls-files -s -z | tr '\0' '\n' | awk '$1 == "160000" { print substr($0, index($0, "\t") + 1) }' >"$td/nested"
git -C "$wt" ls-files -z --others --exclude-standard >"$td/others"
tr '\0' '\n' <"$td/others" | { grep '/$' || true; } | sed 's|/$||' >>"$td/nested"
# The ignored /plugins/ files are hashed, so a repository there counts too.
git -C "$wt" ls-files -o -i --exclude-standard -- plugins | { grep '/$' || true; } | sed 's|/$||' >>"$td/nested"
if [ -s "$td/nested" ]; then
	LC_ALL=C sort -u "$td/nested" | while IFS= read -r n; do
		echo "tree-hash: $n is a nested git repository; this script cannot vouch for its files" >&2
	done
	exit 2
fi

if [ "$mode" = stat ]; then
	cd "$wt"
	{
		git ls-files -z --cached
		cat "$td/others"
		git ls-files -z -o -i --exclude-standard -- plugins
	} >"$td/list"
	# Files gone from disk make stat complain, as does a tracked file whose
	# parent directory became a file; xargs then exits 123 (1 on macOS's BSD
	# xargs). Any other complaint or exit is a real error.
	rc=0
	xargs -0 stat -f '%Fm %Fc %z %N' -- <"$td/list" >"$td/out" 2>"$td/errs" || rc=$?
	# A non-zero exit passes only with a non-empty stderr made of those lines.
	if [ "$rc" -ne 0 ] && { { [ "$rc" -ne 123 ] && [ "$rc" -ne 1 ]; } || [ ! -s "$td/errs" ] || grep -qvE ': (No such file or directory|Not a directory)$' "$td/errs"; }; then
		cat "$td/errs" >&2
		echo "tree-hash: stat failed (exit $rc)" >&2
		exit 1
	fi
	git hash-object --stdin <"$td/out"
	exit 0
fi

idx=$td/index
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
	: >"$idx"
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
