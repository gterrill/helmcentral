#!/bin/sh
# Fail when a change lands inside a section of CHANGELOG.md that was already
# released. Needs the repository's tags.
#
#   sh scripts/check-changelog.sh
#
# .gitattributes merges CHANGELOG.md with the union driver, so two PRs can add
# to "## [Unreleased]" without a conflict. Union has a blind spot: when a
# release commit renames "## [Unreleased]" to "## [X.Y.Z] - date" and a branch
# added an entry under the old heading, the merge keeps the entry inside the
# released section. Once that version is tagged, the entry is in the wrong
# release and missing from the next.
#
# The check compares every released section at the latest tag (from the newest
# "## [N" heading up to the first compare-link line at the foot) with the same
# lines in the working tree. They must be identical. Comparing with the tag
# keeps main red until a misplaced entry is moved, and moving it passes. An
# entry merged after a release PR but before its tag passes, because that change
# really is in the tagged release.
#
# A tagged section's notes are published, so its wording is frozen too: a reword
# fails this check.
set -eu

# Run against this script's own checkout, wherever it was started.
root=$(cd "$(dirname "$0")/.." && pwd)
file=$root/CHANGELOG.md

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

tag=$(git -C "$root" tag --list 'v*' --sort=-v:refname | head -1)
if [ -z "$tag" ]; then
  echo "check-changelog: no release tag found; fetch the tags" >&2
  exit 1
fi

git -C "$root" show "$tag:CHANGELOG.md" >"$tmp/tagged"

heading=$(grep -m1 '^## \[[0-9]' "$tmp/tagged") || {
  echo "check-changelog: CHANGELOG.md at $tag has no released section" >&2
  exit 1
}

# Print from the first line equal to the heading up to, not including, the
# first compare-link line after it.
block() {
  awk -v heading="$heading" '
    !found && $0 == heading { found = 1 }
    found && /^\[[^]]+\]: / { exit }
    found { print }
  ' "$1"
}

block "$tmp/tagged" >"$tmp/tagged.block"
block "$file" >"$tmp/work.block"

if ! diff -u "$tmp/tagged.block" "$tmp/work.block"; then
  echo "check-changelog: a change landed in a section already released at $tag." >&2
  echo "check-changelog: move any new entry under \"## [Unreleased]\"." >&2
  exit 1
fi

echo "check-changelog: released sections unchanged since $tag"
