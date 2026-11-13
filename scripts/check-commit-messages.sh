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

# A Co-Authored-By trailer naming Claude or Anthropic (this covers
# noreply@anthropic.com), or the "Generated with Claude Code" footer with or
# without brackets or the robot emoji before it.
patterns='Co-Authored-By:.*(Claude|Anthropic)|Generated with \[?Claude Code\]?'

if [ "${1:-}" = "--file" ]; then
  file="${2:?usage: check-commit-messages.sh --file FILE}"
  # Check only what git keeps: nothing from the scissors line down (`git commit
  # -v` puts the diff there) and no # comment lines.
  if sed '/^# -\{24\} >8 -\{24\}$/,$d' "$file" | git stripspace --strip-comments | grep -qiE "$patterns"; then
    echo "commit-msg: remove the Claude attribution (Co-Authored-By: Claude / Generated with Claude Code); the operator forbids it" >&2
    exit 1
  fi
  exit 0
fi

range="${1:-origin/main..HEAD}"
cd "$(dirname "$0")/.."

# Assigned on its own so a bad range fails here, under set -e, rather than
# reading as an empty one.
bad="$(git log --format='%h %s' -i -E --grep="$patterns" "$range")"

if [ -n "$bad" ]; then
  echo "$bad"
  echo "check-commit-messages: the commits above carry Claude attribution; reword them" >&2
  exit 1
fi
echo "check-commit-messages: ok ($range)"
