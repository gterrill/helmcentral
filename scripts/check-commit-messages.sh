#!/bin/sh
# Fail when a commit message carries Claude attribution, which the operator has
# forbidden. Two forms:
#
#   sh scripts/check-commit-messages.sh [RANGE]      every commit in RANGE
#                                                    (default origin/main..HEAD)
#   sh scripts/check-commit-messages.sh --file FILE  one message file, as the
#                                                    commit-msg hook uses it
#
# Agents keep adding a "Co-Authored-By: Claude" trailer or a "Generated with
# [Claude Code]" footer by habit. The hook in .githooks stops it at commit time
# (`make hooks` or `make worktree` turns it on) and CI runs the range form on
# every PR, so a commit made where the hook was off is still caught before the
# merge. Both forms use the one pattern list below. Matching ignores case.
# Exits 1 on a match, listing "<short sha> <subject>" per commit in range mode.
set -eu

patterns='Co-Authored-By:[[:space:]]*Claude|Generated with \[Claude Code\]'

if [ "${1:-}" = "--file" ]; then
  if grep -qiE "$patterns" "${2:?usage: check-commit-messages.sh --file FILE}"; then
    echo "commit-msg: remove the Claude attribution (Co-Authored-By: Claude / Generated with [Claude Code]); the operator forbids it" >&2
    exit 1
  fi
  exit 0
fi

range="${1:-origin/main..HEAD}"
cd "$(dirname "$0")/.."

# Listed first so a bad range fails here, under set -e, rather than reading as
# an empty one.
shas="$(git rev-list "$range")"

bad=0
for sha in $shas; do
  if git log -1 --format=%B "$sha" | grep -qiE "$patterns"; then
    git log -1 --format='%h %s' "$sha"
    bad=1
  fi
done

if [ "$bad" -ne 0 ]; then
  echo "check-commit-messages: the commits above carry Claude attribution; reword them" >&2
  exit 1
fi
echo "check-commit-messages: ok ($range)"
