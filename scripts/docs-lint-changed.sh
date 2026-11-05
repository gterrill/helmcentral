#!/bin/sh
# Lints only the Markdown files under docs/ that differ from the merge base
# with origin/main: committed, uncommitted and untracked, but not deleted.
# Runs from the repo root whatever the caller's directory, because vale finds
# its config from the working directory. `make docs-lint` lints everything.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

sh scripts/check-adr-numbers.sh
command -v vale >/dev/null || { echo "docs-lint: vale not installed (brew install vale)"; exit 1; }

base=$(git merge-base origin/main HEAD)
changed=$(git diff --name-only --diff-filter=d "$base" -- docs)
untracked=$(git ls-files --others --exclude-standard -- docs)
# The pipeline's status is sort's, so grep matching nothing is not a failure.
files=$(printf '%s\n%s\n' "$changed" "$untracked" | grep '\.md$' | sort -u)
if [ -z "$files" ]; then
	echo "docs-lint-changed: no changed Markdown files under docs/"
	exit 0
fi

# One file per line; names here hold no spaces.
set -f
# shellcheck disable=SC2086
set -- $files
set +f
echo "docs-lint-changed: $# file(s)"
printf '  %s\n' "$@"
vale --no-global "$@"
